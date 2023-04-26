package node

import (
	"fmt"
	"time"

	"github.com/CESSProject/cess-bucket/pkg/utils"
	"github.com/CESSProject/sdk-go/core/rule"
	"github.com/centrifuge/go-substrate-rpc-client/types"
	"github.com/pkg/errors"
)

func (n *Node) SubscribeNewHeads(ch chan<- bool) {
	defer func() {
		ch <- true
		if err := recover(); err != nil {
			n.Log.Pnc(utils.RecoverError(err))
		}
	}()

	for {

		if n.Cli.GetChainState() {

			sub, err := n.Cli.GetSubstrateAPI().RPC.Chain.SubscribeNewHeads()
			if err != nil {
				time.Sleep(rule.BlockInterval)
				continue
			}
			defer sub.Unsubscribe()

			for {
				head := <-sub.Chan()
				fmt.Printf("Chain is at block: #%v\n", head.Number)
				blockhash, err := n.Cli.GetSubstrateAPI().RPC.Chain.GetBlockHash(head.Number)
				if err != nil {
					continue
				}
				h, err := n.Cli.GetSubstrateAPI().RPC.State.GetStorageRaw(c.keyEvents, blockhash)
				if err != nil {
					return txhash, FileHash{}, errors.Wrap(err, "[GetStorageRaw]")
				}

				types.EventRecordsRaw(*h).DecodeEventRecords(c.metadata, &events)
			}
		}
	}
}