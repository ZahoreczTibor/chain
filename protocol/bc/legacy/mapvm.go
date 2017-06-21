package legacy

import (
	"encoding/binary"

	"chain/protocol/txvm"
	"chain/protocol/txvm/op"

	"chain/protocol/bc"
	"chain/protocol/vm"
	"chain/protocol/vm/vmutil"
)

func MapVMTx(oldTx *TxData) *txvm.Tx {
	tx := &txvm.Tx{
		MinTime: oldTx.MinTime,
		MaxTime: oldTx.MaxTime,
	}

	argsProgs := make([][]byte, len(oldTx.Inputs))

	// OpAnchor:
	// nonce + program + timerange => anchor + condition
	for _, oldinp := range oldTx.Inputs {
		switch ti := oldinp.TypedInput.(type) {
		case *IssuanceInput:
			oldIss := ti
			if len(oldIss.Nonce) > 0 {
				tr := bc.NewTimeRange(oldTx.MinTime, oldTx.MaxTime)

				b := vmutil.NewBuilder()
				b.AddData(oldIss.Nonce)
				b.AddOp(vm.OP_DROP)
				b.AddOp(vm.OP_ASSET)
				b.AddData(oldIss.AssetID().Bytes())
				b.AddOp(vm.OP_EQUAL)
				prog, _ := b.Build() // error is impossible

				trID := bc.EntryID(tr)

				nonceID := bc.EntryID(bc.NewNonce(&bc.Program{VmVersion: 1, Code: prog}, &trID))
				tx.Nonce = append(tx.Nonce, txvm.ID(nonceID.Byte32()))

				pushInt64(&tx.Proof, int64(oldTx.MinTime))
				pushInt64(&tx.Proof, int64(oldTx.MaxTime))
				pushBytes(&tx.Proof, prog)
				tx.Proof = append(tx.Proof, op.Anchor) // nonce => anchor + cond

				var argsProg []byte
				argsProg = append(argsProg, op.Satisfy)
				argsProgs = append(argsProgs, argsProg)
			}

			pushID(&tx.Proof, hashData(oldIss.AssetDefinition).Byte32())
			pushBytes(&tx.Proof, oldIss.IssuanceProgram)
			pushID(&tx.Proof, oldIss.InitialBlock.Byte32())
			pushID(&tx.Proof, hashData(oldinp.ReferenceData).Byte32())
			pushInt64(&tx.Proof, int64(oldIss.Amount))
			pushID(&tx.Proof, oldIss.AssetID().Byte32())
			tx.Proof = append(tx.Proof, op.VM1Issue) // anchor => value + cond

			var argsProg []byte
			for _, arg := range oldIss.Arguments {
				pushBytes(&argsProg, arg)
			}
			pushInt64(&argsProg, int64(len(oldIss.Arguments)))
			argsProg = append(argsProg, op.List)
			argsProg = append(argsProg, op.Satisfy)
			argsProgs = append(argsProgs, argsProg)
		case *SpendInput:
			oldSp := ti
			// output id
			prog := &bc.Program{VmVersion: oldSp.VMVersion, Code: oldSp.ControlProgram}
			src := &bc.ValueSource{
				Ref:      &oldSp.SourceID,
				Value:    &oldSp.AssetAmount,
				Position: oldSp.SourcePosition,
			}
			// ordinal doesn't matter for prevouts, only for result outputs
			prevoutID := bc.EntryID(bc.NewOutput(src, prog, &oldSp.RefDataHash, 0))
			tx.In = append(tx.In, prevoutID.Byte32())

			// proof

			var argsProg []byte
			for _, arg := range oldSp.Arguments {
				pushBytes(&argsProg, arg)
			}
			pushInt64(&argsProg, int64(len(oldSp.Arguments)))
			argsProg = append(argsProg, op.List)
			argsProg = append(argsProg, op.Satisfy)
			argsProgs = append(argsProgs, argsProg)

			// prevout fields
			pushID(&tx.Proof, oldSp.RefDataHash.Byte32())
			pushBytes(&tx.Proof, oldSp.ControlProgram)
			pushInt64(&tx.Proof, int64(oldSp.SourcePosition))
			pushInt64(&tx.Proof, int64(oldSp.AssetAmount.Amount))
			pushID(&tx.Proof, oldSp.AssetAmount.AssetId.Byte32())
			pushID(&tx.Proof, oldSp.SourceID.Byte32())

			// spend input fields
			pushID(&tx.Proof, hashData(oldinp.ReferenceData).Byte32())

			// prevout id + data => vm1value + condition
			tx.Proof = append(tx.Proof, op.VM1Unlock)
		}
	}

	pushInt64(&tx.Proof, int64(len(oldTx.Inputs)))
	tx.Proof = append(tx.Proof, op.VM1Mux)

	// loop in reverse so that output 0 is at the top
	for i := len(oldTx.Outputs) - 1; i >= 0; i-- {
		oldout := oldTx.Outputs[i]
		pushInt64(&tx.Proof, int64(oldout.Amount))
		pushID(&tx.Proof, oldout.AssetId.Byte32())
		tx.Proof = append(tx.Proof, op.VM1Withdraw)
		pushID(&tx.Proof, hashData(oldout.ReferenceData).Byte32())
		if isRetirement(oldout.ControlProgram) {
			tx.Proof = append(tx.Proof, op.Retire)
		} else {
			pushBytes(&tx.Proof, oldout.ControlProgram)
			tx.Proof = append(tx.Proof, op.Lock) // retains output object for checkoutput
		}
	}

	for i := len(argsProgs) - 1; i >= 0; i-- {
		tx.Proof = append(tx.Proof, argsProgs[i]...)
	}

	return tx
}

func isRetirement(prog []byte) bool {
	return len(prog) > 0 && prog[0] == byte(vm.OP_FAIL)
}

func data(p []byte) (g []byte) {
	n := int64(len(p)) + op.BaseData
	g = append(g, encVarint(n)...)
	g = append(g, p...)
	return g
}

func pushInt64(g *[]byte, n int64) {
	*g = append(*g, data(encVarint(n))...)
	*g = append(*g, op.Varint)
}

func pushBytes(g *[]byte, p []byte) {
	*g = append(*g, data(p)...)
}

func pushID(g *[]byte, id [32]byte) {
	pushBytes(g, id[:])
}

func encVarint(v int64) []byte {
	b := make([]byte, 10)
	b = b[:binary.PutUvarint(b, uint64(v))]
	return b
}
