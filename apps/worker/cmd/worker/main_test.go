package main

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/x-web3/worker/internal/chain"
	"github.com/x-web3/worker/internal/indexer"
)

func TestIndexerLogDecoderIgnoreSemantics(t *testing.T) {
	decoder := indexerLogDecoder{}
	mismatch := indexer.LogRecord{Topics: []common.Hash{common.HexToHash("0x1")}}
	if inputs, ignore, err := decoder.Decode(context.Background(), 31337, nil, mismatch); err != nil || !ignore || len(inputs) != 0 {
		t.Fatalf("mismatch = (%d, %v, %v), want ignored", len(inputs), ignore, err)
	}

	rec := indexer.LogRecord{
		Address: common.HexToAddress("0xe7f1725E7734CE288F8367e1Bb143E90bb3F0512"),
		Topics: []common.Hash{
			chain.CoursePurchasedTopic,
			common.HexToHash("0x946131f6a7983f12ad2a94b0ef106f60cf8d188489ddd37cd1020e59be9fc8e7"),
			common.HexToHash("0x000000000000000000000000f39fd6e51aad88f6f4ce6ab8827279cfffb92266"),
		},
		Data: common.FromHex("0x0000000000000000000000005fbdb2315678afecb367f032d93f642f64180aa30000000000000000000000000000000000000000000000056bc75e2d63100000a97732ab860c4f74a2f552201ae27816000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001"),
	}
	inputs, ignore, err := decoder.Decode(context.Background(), 31337, nil, rec)
	if err != nil || ignore || len(inputs) != 1 {
		t.Fatalf("purchase = (%d, %v, %v), want one non-ignored input", len(inputs), ignore, err)
	}
}
