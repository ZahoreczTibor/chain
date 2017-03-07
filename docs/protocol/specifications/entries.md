# Entries Specification

<!-- TOC depthFrom:1 depthTo:6 withLinks:1 updateOnSave:1 orderedList:0 -->

- [Entries Specification](#entries-specification)
	- [Entry Serialization](#entry-serialization)
	- [Fields](#fields)
		- [Byte](#byte)
		- [Hash](#hash)
		- [Integer](#integer)
		- [String](#string)
		- [List](#list)
		- [Struct](#struct)
		- [Extstruct](#extstruct)
		- [Pointer](#pointer)
	- [Data Structures](#data-structures)
		- [Asset Definition](#asset-definition)
		- [AssetAmount](#assetamount)
		- [Program](#program)
			- [Program Validation](#program-validation)
		- [ValueSource](#valuesource)
			- [ValueSource Validation](#valuesource-validation)
		- [ValueDestination](#valuedestination)
			- [ValueDestination Validation](#valuedestination-validation)
	- [Entries](#entries)
		- [TxHeader](#txheader)
			- [TxHeader Body](#txheader-body)
			- [TxHeader Witness](#txheader-witness)
			- [TxHeader Validation](#txheader-validation)
			- [TxHeader Application to State](#txheader-application-to-state)
		- [Output 1](#output-1)
			- [Output Body](#output-body)
			- [Output Witness](#output-witness)
			- [Output Validation](#output-validation)
			- [Retirement 1](#retirement-1)
			- [Retirement Body](#retirement-body)
			- [Retirement Witness](#retirement-witness)
			- [Retirement Validation](#retirement-validation)
		- [Spend 1](#spend-1)
			- [Spend Body](#spend-body)
			- [Spend Witness](#spend-witness)
			- [Spend Validation](#spend-validation)
			- [Spend Application to State](#spend-application-to-state)
		- [Issuance 1](#issuance-1)
			- [Issuance Body](#issuance-body)
			- [Issuance Witness](#issuance-witness)
			- [Issuance Validation](#issuance-validation)
		- [Nonce](#nonce)
			- [Nonce Body](#nonce-body)
			- [Nonce Witness](#nonce-witness)
			- [Nonce Validation](#nonce-validation)
			- [Nonce Application to State](#nonce-application-to-state)
		- [TimeRange](#timerange)
			- [TimeRange Body](#timerange-body)
			- [TimeRange Witness](#timerange-witness)
			- [TimeRange Validation](#timerange-validation)
		- [Mux 1](#mux-1)
			- [Mux Body](#mux-body)
			- [Mux Witness](#mux-witness)
			- [Mux Validation](#mux-validation)

<!-- /TOC -->

This is a specification of the semantic data structures used by transactions. These data structures and rules are used for validation and hashing. This format is independent from the format for [transaction wire serialization](data.md#transaction-wire-serialization).

A transaction is composed of a set of [transaction entries](#entries). Each transaction must include a [Header Entry](#header-entry), which references other entries in the transaction, which in turn can reference additional entries. Entries can be identified by their [Entry ID](#entry-id).

## Entry Serialization

An entry's ID is based on its type and body. The body is encoded as a [string](#string), and its type is [Varint63](data.md#varint63)-encoded.

```
entryID = HASH("entryid:" || entry.type || ":" || entry.body_hash)
```

## Fields

Each entry contains a defined combination of the following fields: [Byte](#byte), [Hash](#hash), [Integer](#integer), [String](#string), [List](#list), [Struct](#struct), [Extstruct](#extstruct), and [Pointer](#entry-pointer).

Below is the serialization of those fields.

### Byte

A `Byte` is encoded as 1 byte.

### Hash

A `Hash` is encoded as 32 bytes.

### Integer

An `Integer` is encoded a [Varint63](data.md#varint63).

### String

A `String` is encoded as a [Varstring31](data.md#varstring31).

### List

A `List` is encoded as a [Varstring31](data.md#varstring31) containing the serialized items, one by one, as defined by the schema.

### Struct

A `Struct` is encoded as a concatenation of all its serialized fields.

### Extstruct

An `ExtStruct` is encoded as a single 32-byte hash.

### Pointer

A Pointer is encoded as a Hash, and identifies another entry by its ID. It also restricts the possible acceptable types: Pointer<X> must refer to an entry of type X.

A Pointer can be `nil`, in which case it is represented by the all-zero 32-byte hash `0x0000000000000000000000000000000000000000000000000000000000000000`.

## Data Structures

### Asset Definition

Field                 | Type                        | Description
----------------------|-----------------------------|----------------
Initial Block ID      | Hash                        | ID of the genesis block for the blockchain in which this asset is defined.
Asset Reference Data  | Hash                        | Hash of the reference data (formerly known as the "asset definition") for this asset.
Issuance Program      | Program                     | Program that must be satisfied for this asset to be issued.

### AssetAmount

Field            | Type                        | Description
-----------------|-----------------------------|----------------
AssetID          | Hash                        | Asset ID.
Value            | Integer                     | Number of units of the referenced asset.

### Program

Field            | Type                        | Description
-----------------|-----------------------------|----------------
Script           | String                      | The program to be executed.
VM Version       | Integer                     | The VM version to be used when evaluating the program.

#### Program Validation

To validate a program with given `Arguments`:

1. If the `VM Version` is greater than 1:
    1. If the transaction version is 1, validation fails.
    2. If the transaction version is greater than 1, validation succeeds.
2. If the `VM Version` is equal to 1:
    1. Evaluate the `Script` with the given `Arguments` using [VM Version 1](#vm.md).
    2. If the script evaluates successfully, validation succeeds. If the script fails evaluation, validation fails.

### ValueSource

An Entry uses a ValueSource to refer to other Entries that provide inputs to the initial Entry.

Field            | Type                        | Description
-----------------|-----------------------------|----------------
Ref              | Pointer<Issuance|Spend|Mux> | Previous entry referenced by this ValueSource.
Value            | AssetAmount                 | Amount and Asset ID contained in the referenced entry.
Position         | Integer                     | Iff this source refers to a Mux entry, then the Position is the index of an output. If this source refers to an Issuance or Spend entry, then the Position must be 0.

#### ValueSource Validation

1. Verify that `Ref` is present and valid.
2. Define `RefDestination` as follows:
    1. If `Ref` is an Issuance or Spend:
        1. Verify that `Position` is 0.
        2. Define `RefDestination` as `Ref.Destination`.
    2. If `Ref` is a `Mux`:
        1. Verify that `Mux.Destinations` contains at least `Position + 1` ValueDestinations.
        2. Define `RefDestination` as `Mux.Destinations[Position]`.
3. Verify that `RefDestination.Ref` is equal to the ID of the current entry.
4. Verify that `RefDestination.Position` is equal to `SourcePosition`, where `SourcePosition` is defined as follows:
    1. If the current entry being validated is an `Output`, `SourcePosition` is 0.
    2. If the current entry being validated is a `Mux`, `SourcePosition` is the index of this `ValueSource` in the current entry's `Sources`.
5. Verify that `RefDestination.Value` is equal to `Value`.

### ValueDestination

An Entry uses a ValueDestination to refer to other entries that receive value from the current Entry.

Field            | Type                           | Description
-----------------|--------------------------------|----------------
Ref              | Pointer<Output|Retirement|Mux> | Next entry referenced by this ValueSource.
Value            | AssetAmount                    | Amount and Asset ID contained in the referenced entry
Position         | Integer                        | Iff this destination refers to a mux entry, then the Position is one of the mux's numbered Inputs. Otherwise, the position must be 0.

#### ValueDestination Validation

1. Verify that `Ref` is present. (This means it must be reachable by traversing `Results` and `Sources` starting from the TxHeader.)
2. Define `RefSource` as follows:
    1. If `Ref` is an `Output` or `Retirement`:
        1. Verify that `Position` is 0.
        2. Define `RefSource` as `Ref.Source`.
    2. If `Ref` is a `Mux`:
        1. Verify that `Ref.Sources` contains at least `Position + 1` ValueSources.
        2. Define `RefSource` as `Ref.Sources[Position]`.
3. Verify that `RefSource.Ref` is equal to the ID of the current entry.
4. Verify that `RefSource.Position` is equal to `DestinationPosition`, where `DestinationPosition` is defined as follows:
    1. If the current entry being validated is an `Issuance` or `Spend`, `DestinationPosition` is 0.
    2. If the current entry being validated is a `Mux`, `DestinationPosition` is the index of this `ValueDestination` in the current entry's `Destinations`.
5. Verify that `RefSource.Value` is equal to `Value`.


## Entries

All entries have the following structure:

Field               | Type                 | Description
--------------------|----------------------|----------------------------------------------------------
Type                | String               | The type of this Entry. e.g. Issuance, Retirement
Body                | Struct               | Varies by type.
Witness             | Struct               | Varies by type.

### TxHeader

Field               | Type                 | Description
--------------------|----------------------|----------------
Type                | String               | "txheader"
Body                | Struct               | See below.  
Witness             | Struct               | See below.

#### TxHeader Body

Field      | Type                                          | Description
-----------|-----------------------------------------------|-------------------------
Version    | Integer                                       | Transaction version, equals 1.
Results    | List<Pointer<Output|Retirement>>              | A list of pointers to Outputs or Retirements. This list must contain at least one item.
Data       | Hash                                          | Hash of the reference data for the transaction, or a string of 32 zero-bytes (representing no reference data).
Mintime    | Integer                                       | Must be either zero or a timestamp lower than the timestamp of the block that includes the transaction
Maxtime    | Integer                                       | Must be either zero or a timestamp higher than the timestamp of the block that includes the transaction.
ExtHash    | Hash                                          | Hash of all extension fields. (See [Extstruct](#extstruct).) If `Version` is known, this must be 32 zero-bytes.

#### TxHeader Witness

Field               | Type                 | Description
--------------------|----------------------|----------------

#### TxHeader Validation

1. Check that `Results` includes at least one item.
2. Check that each of the `Results` is present and valid.

#### TxHeader Application to State

1. Verify that mintime is lower than the timestamp of the block that includes the transaction.
2. Verify that maxtime is either zero or is higher than the timestamp of the block that includes the transaction.

### Output 1

Field               | Type                 | Description
--------------------|----------------------|----------------
Type                | String               | "output1"
Body                | Struct               | See below.
Witness             | Struct               | See below.

#### Output Body

Field               | Type                 | Description
--------------------|----------------------|----------------
Source              | ValueSource          | The source of the units to be included in this output.
ControlProgram      | Program              | The program to control this output.
Data                | Hash                 | Hash of the reference data for this entry, or a string of 32 zero-bytes (representing no reference data).
ExtHash             | Hash                 | If the transaction version is known, this must be 32 zero-bytes.

#### Output Witness

Field               | Type                 | Description
--------------------|----------------------|----------------

#### Output Validation

1. [Validate](#valuesource-validation) `Source`.

#### Output Application to State

1. Add the entry ID of the Output entry to the UTXO set.

#### Retirement 1

Field               | Type                 | Description
--------------------|----------------------|----------------
Type                | String               | "retirement1"
Body                | Struct               | See below.
Witness             | Struct               | See below.

#### Retirement Body

Field               | Type                 | Description
--------------------|----------------------|----------------
Source              | ValueSource          | The source of the units that are being retired.
Data                | Hash                 | Hash of the reference data for this entry, or a string of 32 zero-bytes (representing no reference data).
ExtHash             | Hash                 | If the transaction version is known, this must be 32 zero-bytes.

#### Retirement Witness

Field               | Type                 | Description
--------------------|----------------------|----------------

#### Retirement Validation

1. [Validate](#valuesource-validation) `Source`.

### Spend 1

Field               | Type                 | Description
--------------------|----------------------|----------------
Type                | String               | "spend1"
Body                | Struct               | See below.
Witness             | Struct               | See below.

#### Spend Body

Field               | Type                 | Description
--------------------|----------------------|----------------
SpentOutput         | Pointer<Output>      | The Output entry consumed by this spend.
Data                | Hash                 | Hash of the reference data for this entry, or a string of 32 zero-bytes (representing no reference data).
ExtHash             | Hash                 | If the transaction version is known, this must be 32 zero-bytes.

#### Spend Witness

Field               | Type                 | Description
--------------------|----------------------|----------------
Destination         | ValueDestination     | The Destination ("forward pointer") for the value contained in this spend. This can point directly to an Output entry, or to a Mux, which points to Output entries via its own Destinations.
Arguments           | List<String>         | Arguments for the control program contained in the SpentOutput.

#### Spend Validation

1. Verify that `SpentOutput` is present in the transaction (do not check that it is valid.)
2. [Validate](#program-validation) `SpentOutput.ControlProgram` with the given `Arguments`.
3. Verify that `SpentOutput.Value` is equal to `Destination.Value`.
4. [Validate](#valuedestination-validation) `Destination`.

#### Spend Application to State

1. Verify that the entry ID of `SpentOutput` is included in the UTXO set.
2. Remove the entry ID of `SpentOutput` from the UTXO set.

### Issuance 1

Field               | Type                 | Description
--------------------|----------------------|----------------
Type                | String               | "issuance1"
Body                | Struct               | See below.
Witness             | Struct               | See below.

#### Issuance Body

Field               | Type                 | Description
--------------------|----------------------|----------------
Anchor              | Pointer<Nonce|Spend> | Used to guarantee uniqueness of this entry.
Value               | AssetAmount          | Asset ID and amount being issued.
Data                | Hash                 | Hash of the reference data for this entry, or a string of 32 zero-bytes (representing no reference data).
ExtHash             | Hash                 | If the transaction version is known, this must be 32 zero-bytes.

#### Issuance Witness

Field               | Type                                      | Description
--------------------|-------------------------------------------|----------------
Destination         | ValueDestination                          | The Destination ("forward pointer") for the value contained in this spend. This can point directly to an Output Entry, or to a Mux, which points to Output Entries via its own Destinations.
AssetDefinition     | [Asset Definition](#asset-definition)     | Asset definition for the asset being issued.
Arguments           | List<String>                              | Arguments for the control program contained in the SpentOutput.

#### Issuance Validation

1. Verify that `AssetDefinition` hashes to `Value.AssetID`.
2. [Validate](#program-validation) `AssetDefinition.Program` with the given `Arguments`.
3. Verify that `Anchor` is present and valid.
4. [Validate](#valuedestination-validation) `Destination`.

### Nonce  

Field               | Type                 | Description
--------------------|----------------------|----------------
Type                | String               | "nonce"
Body                | Struct               | See below.
Witness             | Struct               | See below.

#### Nonce Body

Field               | Type                 | Description
--------------------|----------------------|----------------
Program             | Program              | A program that protects the nonce against replay and must evaluate to true.
Time Range          | Pointer<TimeRange>   | Reference to a TimeRange entry.
ExtHash             | Hash                 | If the transaction version is known, this must be 32 zero-bytes.

#### Nonce Witness

Field               | Type                         | Description
--------------------|------------------------------|----------------
Arguments           | List<String>                 | Arguments for the program contained in the Nonce.
Issuance            | Pointer<Issuance>            | Pointer to an issuance entry.

#### Nonce Validation

1. [Validate](#program-validation) `Program` with the given `Arguments`.
2. Verify that `Issuance` points to an issuance that is present in the transaction (meaning visitable by traversing `Results` and `Sources` from the transaction header) and whose `Anchor` is equal to this nonce's ID.

#### Nonce Application to State

1. Verify that the entry ID of the Nonce is not included in the Nonce set.
2. Add the entry ID of the Nonce to the Nonce set.

### TimeRange  

Field               | Type                 | Description
--------------------|----------------------|----------------
Type                | String               | "timerange"
Body                | Struct               | See below.
Witness             | Struct               | See below.

#### TimeRange Body

Field               | Type                 | Description
--------------------|----------------------|----------------
Mintime             | Integer              | Minimum time for this transaction.
Maxtime             | Integer              | Maximum time for this transaction.
ExtHash             | Hash                 | If the transaction version is known, this must be 32 zero-bytes.

#### TimeRange Witness

Field               | Type                 | Description
--------------------|----------------------|----------------

#### TimeRange Validation

1. Verify that `Mintime` is equal to or less than the `Mintime` specified in the transaction header.
2. Verify that `Maxtime` is either zero, or is equal to or greater than the `Maxtime` specified in the transaction header.

### Mux 1

Field               | Type                 | Description
--------------------|----------------------|----------------
Type                | String               | "mux1"
Body                | Struct               | See below.
Witness             | Struct               | See below.

#### Mux Body

Field               | Type                 | Description
--------------------|----------------------|----------------
Sources             | List<ValueSource>    | The source of the units to be included in this Mux.
Program             | Program              | A program that controls the value in the Mux and must evaluate to true.
ExtHash             | Hash                 | If the transaction version is known, this must be 32 zero-bytes.

#### Mux Witness

Field               | Type                       | Description
--------------------|----------------------------|----------------
Destinations        | List<ValueDestination>     | The Destinations ("forward pointers") for the value contained in this Mux. This can point directly to Output entries, or to other Muxes, which point to Output entries via their own Destinations.
Arguments           | String                     | Arguments for the program contained in the Nonce.

#### Mux Validation

1. [Validate](#program-validation) `Program` with the given `Arguments`.
2. For each `Source` in `Sources`, [validate](#valuesource-validation) `Source`.
3. For each `Destination` in `Destinations`, [validate](#valuedestination-validation) `Destination`.
4. For each `AssetID` represented in `Sources` and `Destinations`:
    1. Sum the total `Amounts` of the `Sources` with that asset ID.
    2. Sum the total `Amounts` of the `Destinations` with that asset ID.
    3. Verify that the two sums are equal.