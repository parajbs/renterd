package api

import (
	"errors"
	"math/big"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
	"go.sia.tech/renterd/object"
)

// ConsensusState holds the current blockheight and whether we are synced or not.
type ConsensusState struct {
	BlockHeight uint64
	Synced      bool
}

// ContractsIDAddRequest is the request type for the /contract/:id endpoint.
type ContractsIDAddRequest struct {
	Contract    rhpv2.ContractRevision `json:"contract"`
	StartHeight uint64                 `json:"startHeight"`
	TotalCost   types.Currency         `json:"totalCost"`
}

// ContractsIDRenewedRequest is the request type for the /contract/:id/renewed
// endpoint.
type ContractsIDRenewedRequest struct {
	Contract    rhpv2.ContractRevision `json:"contract"`
	RenewedFrom types.FileContractID   `json:"renewedFrom"`
	StartHeight uint64                 `json:"startHeight"`
	TotalCost   types.Currency         `json:"totalCost"`
}

// ContractAcquireRequest is the request type for the /contract/acquire
// endpoint.
type ContractAcquireRequest struct {
	Duration ParamDuration `json:"duration"`
	Priority int           `json:"priority"`
}

// ContractAcquireRequest is the request type for the /contract/:id/release
// endpoint.
type ContractReleaseRequest struct {
	LockID uint64 `json:"lockID"`
}

// ContractAcquireResponse is the response type for the /contract/:id/acquire
// endpoint.
type ContractAcquireResponse struct {
	LockID uint64 `json:"lockID"`
}

// HostsRemoveRequest is the request type for the /hosts/remove endpoint.
type HostsRemoveRequest struct {
	MinRecentScanFailures uint64            `json:"minRecentScanFailures"`
	MaxDowntimeHours      ParamDurationHour `json:"maxDowntimeHours"`
}

// WalletFundRequest is the request type for the /wallet/fund endpoint.
type WalletFundRequest struct {
	Transaction types.Transaction `json:"transaction"`
	Amount      types.Currency    `json:"amount"`
}

// WalletFundResponse is the response type for the /wallet/fund endpoint.
type WalletFundResponse struct {
	Transaction types.Transaction   `json:"transaction"`
	ToSign      []types.Hash256     `json:"toSign"`
	DependsOn   []types.Transaction `json:"dependsOn"`
}

// WalletSignRequest is the request type for the /wallet/sign endpoint.
type WalletSignRequest struct {
	Transaction   types.Transaction   `json:"transaction"`
	ToSign        []types.Hash256     `json:"toSign"`
	CoveredFields types.CoveredFields `json:"coveredFields"`
}

// WalletRedistributeRequest is the request type for the /wallet/redistribute
// endpoint.
type WalletRedistributeRequest struct {
	Amount  types.Currency `json:"amount"`
	Outputs int            `json:"outputs"`
}

// WalletPrepareFormRequest is the request type for the /wallet/prepare/form
// endpoint.
type WalletPrepareFormRequest struct {
	EndHeight      uint64             `json:"endHeight"`
	HostCollateral types.Currency     `json:"hostCollateral"`
	HostKey        types.PublicKey    `json:"hostKey"`
	HostSettings   rhpv2.HostSettings `json:"hostSettings"`
	RenterAddress  types.Address      `json:"renterAddress"`
	RenterFunds    types.Currency     `json:"renterFunds"`
	RenterKey      types.PrivateKey   `json:"renterKey"`
}

// WalletPrepareRenewRequest is the request type for the /wallet/prepare/renew
// endpoint.
type WalletPrepareRenewRequest struct {
	Contract      types.FileContractRevision `json:"contract"`
	EndHeight     uint64                     `json:"endHeight"`
	HostKey       types.PublicKey            `json:"hostKey"`
	HostSettings  rhpv2.HostSettings         `json:"hostSettings"`
	NewCollateral types.Currency             `json:"newCollateral"`
	RenterAddress types.Address              `json:"renterAddress"`
	RenterFunds   types.Currency             `json:"renterFunds"`
	RenterKey     types.PrivateKey           `json:"renterKey"`
}

// WalletPrepareRenewResponse is the response type for the /wallet/prepare/renew
// endpoint.
type WalletPrepareRenewResponse struct {
	TransactionSet []types.Transaction `json:"transactionSet"`
	FinalPayment   types.Currency      `json:"finalPayment"`
}

// ObjectsResponse is the response type for the /objects endpoint.
type ObjectsResponse struct {
	Entries []string       `json:"entries,omitempty"`
	Object  *object.Object `json:"object,omitempty"`
}

// AddObjectRequest is the request type for the /object/*key endpoint.
type AddObjectRequest struct {
	Object        object.Object                            `json:"object"`
	UsedContracts map[types.PublicKey]types.FileContractID `json:"usedContracts"`
}

// MigrationSlabsRequest is the request type for the /slabs/migration endpoint.
type MigrationSlabsRequest struct {
	ContractSet string `json:"contractset"`
	Limit       int    `json:"limit"`
}

// UpdateSlabRequest is the request type for the /slab endpoint.
type UpdateSlabRequest struct {
	Slab          object.Slab                              `json:"slab"`
	UsedContracts map[types.PublicKey]types.FileContractID `json:"usedContracts"`
}

// UpdateAllowlistRequest is the request type for /hosts/allowlist endpoint.
type UpdateAllowlistRequest struct {
	Add    []types.PublicKey `json:"add"`
	Remove []types.PublicKey `json:"remove"`
}

// UpdateBlocklistRequest is the request type for /hosts/blocklist endpoint.
type UpdateBlocklistRequest struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

// AccountsUpdateBalanceRequest is the request type for /accounts/:id/update
// endpoint.
type AccountsUpdateBalanceRequest struct {
	Host   types.PublicKey `json:"host"`
	Owner  ParamString     `json:"owner"`
	Amount *big.Int        `json:"amount"`
}

// AccountsAddBalanceRequest is the request type for /accounts/:id/add
// endpoint.
type AccountsAddBalanceRequest struct {
	Host   types.PublicKey `json:"host"`
	Owner  ParamString     `json:"owner"`
	Amount *big.Int        `json:"amount"`
}

// DownloadParams contains the metadata needed by a worker to download an object.
type DownloadParams struct {
	ContractSet string
	GougingParams
}

// UploadParams contains the metadata needed by a worker to upload an object.
type UploadParams struct {
	CurrentHeight uint64
	ContractSet   string
	GougingParams
}

// GougingParams contains the metadata needed by a worker to perform gouging
// checks.
type GougingParams struct {
	GougingSettings    GougingSettings
	RedundancySettings RedundancySettings
}

// GougingSettings contain some price settings used in price gouging.
type GougingSettings struct {
	MinMaxCollateral types.Currency
	MaxRPCPrice      types.Currency
	MaxContractPrice types.Currency
	MaxDownloadPrice types.Currency // per TiB
	MaxUploadPrice   types.Currency // per TiB
	MaxStoragePrice  types.Currency // per byte per block
}

// RedundancySettings contain settings that dictate an object's redundancy.
type RedundancySettings struct {
	MinShards   int
	TotalShards int
}

// Redundancy returns the effective storage redundancy of the
// RedundancySettings.
func (rs RedundancySettings) Redundancy() float64 {
	return float64(rs.TotalShards) / float64(rs.MinShards)
}

// Validate returns an error if the redundancy settings are not considered
// valid.
func (rs RedundancySettings) Validate() error {
	if rs.MinShards < 1 {
		return errors.New("MinShards must be greater than 0")
	}
	if rs.TotalShards < rs.MinShards {
		return errors.New("TotalShards must be at least MinShards")
	}
	if rs.TotalShards > 255 {
		return errors.New("TotalShards must be less than 256")
	}
	return nil
}

// ErrSettingNotFound is returned if a requested setting is not present in the
// database.
var ErrSettingNotFound = errors.New("setting not found")
