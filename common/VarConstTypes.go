package common

import "reflect"

var (
	//VaultURL                string
	SecShares               int
	SecThreshold            int
	WaitTimeSeconds         int
	ReadinessProbeInSeconds int
)

const (
	WaitTime               = 3
	ReadinessProbe         = 5
	DefaultSecretShares    = 5
	DefaultSecretThreshold = 3
	HttpMethodGET          = "GET"
	HttpMethodPOST         = "POST"
	HttpMethodPUT          = "PUT"
	VaultKeysSecretName    = "vault-init-keys"
)

type VaultInitResp struct {
	Keys       []string `json:"keys"`
	KeysBase64 []string `json:"keys_base64"`
	RootToken  string   `json:"root_token"`
}

type VaultUnsealResp struct {
	Type         string `json:"type"`
	Initialized  bool   `json:"initialized"`
	Sealed       bool   `json:"sealed"`
	T            int    `json:"t"`
	N            int    `json:"n"`
	Progress     int    `json:"progress"`
	Nonce        string `json:"nonce"`
	Version      string `json:"version"`
	Migration    bool   `json:"migration"`
	ClusterName  string `json:"cluster_name"`
	ClusterID    string `json:"cluster_id"`
	RecoverySeal bool   `json:"recovery_seal"`
	StorageType  string `json:"storage_type"`
}

func (parsedKeys VaultInitResp) IsEmpty() bool {
	return reflect.DeepEqual(parsedKeys, VaultInitResp{})
}
