package api

import (
	"context"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func AccountInfo(apiAddress string, accountAddress types.Address) (*spacemeshv2alpha1.Account, error) {
	conn, err := connect(apiAddress)
	if err != nil {
		return nil, err
	}

	req := spacemeshv2alpha1.AccountRequest{
		Addresses: []string{accountAddress.String()},
		Limit:     1,
	}

	resp, err := spacemeshv2alpha1.NewAccountServiceClient(conn).List(context.Background(), &req)
	if err != nil {
		return nil, err
	}

	return resp.Accounts[0], nil
}
