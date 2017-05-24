package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"chain/core/config"
	"chain/core/fetch"
	"chain/core/leader"
	"chain/database/pg"
	"chain/database/sinkdb"
	"chain/errors"
	"chain/log"
	"chain/net/http/httpjson"
	"chain/net/raft"
	"chain/protocol/bc"
)

var (
	errAlreadyConfigured = errors.New("core is already configured; must reset first")
	errUnconfigured      = errors.New("core is not configured")
	errNoMockHSM         = errors.New("core is not configured with a mockhsm")
	errNoReset           = errors.New("core is not configured with reset capabilities")
	errBadBlockPub       = errors.New("supplied block pub key is invalid")
	errNoClientTokens    = errors.New("cannot enable client auth without client access tokens")
)

const (
	crosscoreRPCVersion = 3
	clusterIDKey        = `/core/cluster_id`
)

func (a *API) reset(ctx context.Context, req struct {
	Everything bool `json:"everything"`
}) error {
	dataToReset := "blockchain"
	if req.Everything {
		dataToReset = "everything"
	}

	closeConnOK(httpjson.ResponseWriter(ctx), httpjson.Request(ctx))
	execSelf(dataToReset)
	panic("unreached")
}

func (a *API) info(ctx context.Context) (map[string]interface{}, error) {
	if a.config == nil {
		// never configured
		return map[string]interface{}{
			"is_configured": false,
			"version":       config.Version,
			"build_commit":  config.BuildCommit,
			"build_date":    config.BuildDate,
			"build_config":  config.BuildConfig,
		}, nil
	}
	// If we're not the leader, forward to the leader.
	if a.leader.State() == leader.Following {
		var resp map[string]interface{}
		err := a.forwardToLeader(ctx, "/info", nil, &resp)
		return resp, err
	}
	return a.leaderInfo(ctx)
}

func (a *API) leaderInfo(ctx context.Context) (map[string]interface{}, error) {
	var generatorHeight uint64
	var generatorFetched time.Time

	a.downloadingSnapshotMu.Lock()
	snapshot := a.downloadingSnapshot
	a.downloadingSnapshotMu.Unlock()

	localHeight := a.chain.Height()

	if a.config.IsGenerator {
		now := time.Now()
		generatorHeight = localHeight
		generatorFetched = now
	} else {
		fetchHeight, fetchTime := fetch.GeneratorHeight()
		// Because everything is asynchronous, it's possible for the localHeight to
		// be higher than our cached generator height. In that case, display the
		// local height as the generator height.
		if localHeight > fetchHeight {
			fetchHeight = localHeight
		}

		// fetchTime might be the zero time if we're having trouble connecting
		// to the remote generator. Only set the height & time if we have it.
		// The dashboard will handle zeros correctly.
		if !fetchTime.IsZero() {
			generatorHeight, generatorFetched = fetchHeight, fetchTime
		}
	}

	var (
		configuredAtSecs  int64 = int64(a.config.ConfiguredAt / 1000)
		configuredAtNSecs int64 = int64((a.config.ConfiguredAt % 1000) * 1e6)
	)

	m := map[string]interface{}{
		"state":                             a.leader.State().String(),
		"is_configured":                     true,
		"configured_at":                     time.Unix(configuredAtSecs, configuredAtNSecs).UTC(),
		"is_signer":                         a.config.IsSigner,
		"is_generator":                      a.config.IsGenerator,
		"generator_url":                     a.config.GeneratorUrl,
		"generator_access_token":            obfuscateTokenSecret(a.config.GeneratorAccessToken),
		"blockchain_id":                     a.config.BlockchainId,
		"block_height":                      localHeight,
		"generator_block_height":            generatorHeight,
		"generator_block_height_fetched_at": generatorFetched,
		"network_rpc_version":               crosscoreRPCVersion, // "Network" is legacy terminology for "Cross-core"
		"crosscore_rpc_version":             crosscoreRPCVersion,
		"core_id":                           a.config.Id,
		"version":                           config.Version,
		"build_commit":                      config.BuildCommit,
		"build_date":                        config.BuildDate,
		"build_config":                      config.BuildConfig,
		"health":                            a.health(),
	}

	// Add in snapshot information if we're downloading a snapshot.
	if snapshot != nil {
		downloadedBytes, totalBytes := snapshot.Progress()
		m["snapshot"] = map[string]interface{}{
			"attempt":     snapshot.Attempt(),
			"height":      snapshot.Height(),
			"size":        totalBytes,
			"downloaded":  downloadedBytes,
			"in_progress": true,
		}
	}
	return m, nil
}

func (a *API) configure(ctx context.Context, x *config.Config) error {
	if a.config != nil {
		return errAlreadyConfigured
	}

	if x.IsGenerator && x.MaxIssuanceWindowMs == 0 {
		x.MaxIssuanceWindowMs = bc.DurationMillis(24 * time.Hour)
	}

	err := config.Configure(ctx, a.db, a.sdb, a.httpClient, x)
	if err != nil {
		return err
	}

	closeConnOK(httpjson.ResponseWriter(ctx), httpjson.Request(ctx))
	execSelf("")
	panic("unreached")
}

func (a *API) initCluster(ctx context.Context) error {
	err := a.sdb.RaftService().Init()
	if err != nil {
		return err
	}

	// add this process's address as an allowed member
	err = a.addAllowedMember(ctx, struct{ Addr string }{a.addr})
	if err != nil {
		return err
	}

	// TODO(jackson): make adding this process's address and
	// setting the cluster ID atomic with initializing the cluster

	return setClusterID(ctx, a.sdb, a.db)
}

func (a *API) joinCluster(ctx context.Context, x struct {
	BootAddress string `json:"boot_address"`
}) error {
	// validate the format of the boot address
	_, _, err := net.SplitHostPort(x.BootAddress)
	if err != nil {
		newerr := errors.Sub(errInvalidAddr, err)
		if addrErr, ok := err.(*net.AddrError); ok {
			newerr = errors.WithDetail(newerr, addrErr.Err)
		}
		return newerr
	}

	bootURL := fmt.Sprintf("https://%s", x.BootAddress)
	return a.sdb.RaftService().Join(bootURL)
}

// setClusterID sets the Core's cluster ID in both sinkdb and Postgres.
func setClusterID(ctx context.Context, sdb *sinkdb.DB, db pg.DB) error {
	var clusterID [8]byte
	_, err := rand.Read(clusterID[:])
	if err != nil {
		return err
	}

	err = sdb.Exec(ctx,
		sinkdb.IfNotExists(clusterIDKey),
		sinkdb.Set(clusterIDKey, &sinkdb.Bytes{Value: clusterID[:]}),
	)
	if err != nil {
		return err
	}

	const q = `INSERT INTO cluster (id) VALUES($1)`
	_, err = db.ExecContext(ctx, q, clusterID[:])
	return err
}

// VerifyClusterID guards against the scenario where the Postgres
// database is reset without resetting sinkdb.
// https://github.com/chain/chain/issues/1167
func VerifyClusterID(ctx context.Context, sdb *sinkdb.DB, db pg.DB) error {
	// Lookup the existing cluster IDs, if any.
	var sdbClusterID sinkdb.Bytes
	sinkdbFound, err := sdb.Get(ctx, clusterIDKey, &sdbClusterID)
	if err != nil && errors.Root(err) != raft.ErrUninitialized {
		return err
	} else if errors.Root(err) == raft.ErrUninitialized {
		return nil
	}

	var pgClusterID []byte
	err = db.QueryRowContext(ctx, `SELECT id FROM cluster`).Scan(&pgClusterID)
	if err != nil && errors.Root(err) != sql.ErrNoRows {
		return err
	}

	if errors.Root(err) == sql.ErrNoRows && !sinkdbFound {
		// This is an initialized cored process but there's no cluster ID
		// in Postgres or in sinkdb. It's likely a pre-1.3 Core or a Developer
		// Edition core that was auto-initialized with the `init_cluster` build
		// flag. We need to set the cluster ID for the first time.
		return setClusterID(ctx, sdb, db)
	}
	if bytes.Equal(pgClusterID, sdbClusterID.Value) {
		return nil
	}
	return fmt.Errorf("corrupted datastores: mismatched cluster IDs: %x and %x",
		pgClusterID,
		sdbClusterID.Value,
	)
}

func closeConnOK(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Connection", "close")
	w.WriteHeader(http.StatusNoContent)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		log.Printf(req.Context(), "no hijacker")
		return
	}
	conn, buf, err := hijacker.Hijack()
	if err != nil {
		log.Printf(req.Context(), "could not hijack connection: %s\n", err)
		return
	}
	err = buf.Flush()
	if err != nil {
		log.Printf(req.Context(), "could not flush connection buffer: %s\n", err)
	}
	err = conn.Close()
	if err != nil {
		log.Printf(req.Context(), "could not close connection: %s\n", err)
	}
}

func obfuscateTokenSecret(token string) string {
	toks := strings.SplitN(token, ":", 2)
	var res string
	if len(toks) > 0 {
		res += toks[0]
	}
	if len(toks) > 1 {
		res += ":********"
	}
	return res
}
