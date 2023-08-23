/*
	Copyright (C) CESS. All rights reserved.
	Copyright (C) Cumulus Encrypted Storage System. All rights reserved.

	SPDX-License-Identifier: Apache-2.0
*/

package node

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/CESSProject/cess-bucket/configs"
	"github.com/CESSProject/cess-bucket/pkg/cache"
	"github.com/CESSProject/cess-bucket/pkg/confile"
	"github.com/CESSProject/cess-bucket/pkg/logger"
	"github.com/CESSProject/cess-bucket/pkg/proof"
	"github.com/CESSProject/cess-bucket/pkg/utils"
	"github.com/CESSProject/cess-go-sdk/core/pattern"
	"github.com/CESSProject/cess-go-sdk/core/sdk"
	sutils "github.com/CESSProject/cess-go-sdk/core/utils"
	"github.com/CESSProject/p2p-go/out"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mr-tron/base58"
	"github.com/multiformats/go-multiaddr"
)

type Node struct {
	key        *proof.RSAKeyPair
	peerLock   *sync.RWMutex
	teeLock    *sync.RWMutex
	peers      map[string]peer.AddrInfo
	teeWorkers map[string][]byte
	peersPath  string
	sdk.SDK
	confile.Confile
	logger.Logger
	cache.Cache
	*Pois
}

// New is used to build a node instance
func New() *Node {
	return &Node{
		peerLock:   new(sync.RWMutex),
		teeLock:    new(sync.RWMutex),
		peers:      make(map[string]peer.AddrInfo, 0),
		teeWorkers: make(map[string][]byte, 10),
		Pois:       &Pois{},
	}
}

func (n *Node) Run() {
	var (
		ch_spaceMgt         = make(chan bool, 1)
		ch_idlechallenge    = make(chan bool, 1)
		ch_servicechallenge = make(chan bool, 1)
		ch_reportfiles      = make(chan bool, 1)
		ch_calctag          = make(chan bool, 1)
		ch_replace          = make(chan bool, 1)
		ch_resizespace      = make(chan bool, 1)
		//ch_restoreMgt  = make(chan bool, 1)
		ch_discoverMgt = make(chan bool, 1)
	)

	ch_idlechallenge <- true
	ch_servicechallenge <- true
	ch_reportfiles <- true
	ch_calctag <- true
	ch_replace <- true
	ch_resizespace <- true

	// peer persistent location
	n.peersPath = filepath.Join(n.Workspace(), "peers")

	for {
		pubkey, err := n.QueryTeePodr2Puk()
		if err != nil {
			time.Sleep(pattern.BlockInterval)
			continue
		}
		err = n.SetPublickey(pubkey)
		if err != nil {
			time.Sleep(pattern.BlockInterval)
			continue
		}
		n.Schal("info", "Initialize key successfully")
		break
	}

	task_18S := time.NewTicker(time.Duration(time.Second * 18))
	defer task_18S.Stop()

	task_Minute := time.NewTicker(time.Minute)
	defer task_Minute.Stop()

	task_Hour := time.NewTicker(time.Hour)
	defer task_Hour.Stop()

	// go n.restoreMgt(ch_restoreMgt)
	go n.discoverMgt(ch_discoverMgt)

	go n.poisMgt(ch_spaceMgt)

	n.syncChainStatus()

	out.Ok("start successfully")
	var idleChallResult bool
	var serviceChallResult bool
	var idleChallTeeAcc string
	var serviceChallTeeAcc string
	var minerSnapShot pattern.MinerSnapShot_V2
	for {
		select {
		case <-task_18S.C:
			n.Log("info", "Start chal task cycle...")
			err := n.connectChain()
			if err != nil {
				n.Log("err", pattern.ERR_RPC_CONNECTION.Error())
				out.Err(pattern.ERR_RPC_CONNECTION.Error())
				break
			}

			challenge, err := n.QueryChallenge_V2()
			if err != nil {
				if err.Error() != pattern.ERR_Empty {
					n.Ichal("err", fmt.Sprintf("[QueryChallenge] %v", err))
					n.Schal("err", fmt.Sprintf("[QueryChallenge] %v", err))
				}
				break
			}
			n.Log("info", "Query challenge suc")
			for _, v := range challenge.MinerSnapshotList {
				if sutils.CompareSlice(n.GetSignatureAccPulickey(), v.Miner[:]) {
					n.Log("info", "Found challenge unsubmit prove")
					latestBlock, err := n.QueryBlockHeight("")
					if err != nil {
						n.Ichal("err", fmt.Sprintf("[QueryBlockHeight] %v", err))
						n.Schal("err", fmt.Sprintf("[QueryBlockHeight] %v", err))
						break
					}
					challExpiration, err := n.QueryChallengeExpiration()
					if err != nil {
						n.Ichal("err", fmt.Sprintf("[QueryChallengeExpiration] %v", err))
						n.Schal("err", fmt.Sprintf("[QueryChallengeExpiration] %v", err))
						break
					}

					if !v.IdleSubmitted {
						if len(ch_idlechallenge) > 0 {
							_ = <-ch_idlechallenge
							n.Log("info", "start poisChallenge thread")
							go n.poisChallenge(ch_idlechallenge, latestBlock, challExpiration, challenge, v)
						}
					}

					if !v.ServiceSubmitted {
						if len(ch_servicechallenge) > 0 {
							_ = <-ch_servicechallenge
							n.Log("info", "start serviceChallenge thread")
							go n.serviceChallenge(ch_servicechallenge, latestBlock, challExpiration, challenge, v)
						}
					}
					break
				}
			}

			n.Log("info", "start query unverified prove")
			idleChallResult = false
			serviceChallResult = false
			teeAccounts := n.GetAllTeeWorkAccount()
			for _, v := range teeAccounts {
				if idleChallResult && serviceChallResult {
					break
				}
				publickey, err := sutils.ParsingPublickey(v)
				if err != nil {
					continue
				}
				if !idleChallResult {
					idleProofInfos, err := n.QueryUnverifiedIdleProof(publickey)
					if err == nil {
						for i := 0; i < len(idleProofInfos); i++ {
							if sutils.CompareSlice(idleProofInfos[i].MinerSnapShot.Miner[:], n.GetSignatureAccPulickey()) {
								idleChallResult = true
								idleChallTeeAcc = v
								minerSnapShot = idleProofInfos[i].MinerSnapShot
								n.Log("info", "Found unverified idle prove")
								break
							}
						}
					}
				}
				if !serviceChallResult {
					serviceProofInfos, err := n.QueryUnverifiedIdleProof(publickey)
					if err == nil {
						for i := 0; i < len(serviceProofInfos); i++ {
							if sutils.CompareSlice(serviceProofInfos[i].MinerSnapShot.Miner[:], n.GetSignatureAccPulickey()) {
								serviceChallResult = true
								serviceChallTeeAcc = v
								minerSnapShot = serviceProofInfos[i].MinerSnapShot
								n.Log("info", "Found unverified service prove")
								break
							}
						}
					}
				}
			}

			n.Log("info", "Query unverified prove end")
			if idleChallResult || serviceChallResult {
				latestBlock, err := n.QueryBlockHeight("")
				if err != nil {
					n.Ichal("err", fmt.Sprintf("[QueryBlockHeight] %v", err))
					n.Schal("err", fmt.Sprintf("[QueryBlockHeight] %v", err))
					break
				}

				challVerifyExpiration, err := n.QueryChallengeVerifyExpiration()
				if err != nil {
					n.Ichal("err", fmt.Sprintf("[QueryChallengeExpiration] %v", err))
					n.Schal("err", fmt.Sprintf("[QueryChallengeExpiration] %v", err))
					break
				}

				if idleChallResult {
					if len(ch_idlechallenge) > 0 {
						_ = <-ch_idlechallenge
						n.Log("info", "Start poisChallengeResult thread")
						go n.poisChallengeResult(ch_idlechallenge, latestBlock, challVerifyExpiration, idleChallTeeAcc, challenge, minerSnapShot)
					}
				}

				if serviceChallResult {
					if len(ch_servicechallenge) > 0 {
						_ = <-ch_servicechallenge
						n.Log("info", "Start poisServiceChallengeResult thread")
						go n.poisServiceChallengeResult(ch_servicechallenge, latestBlock, challVerifyExpiration, serviceChallTeeAcc, challenge, minerSnapShot)
					}
				}
			}

		case <-task_Minute.C:
			n.syncChainStatus()
			if len(ch_reportfiles) > 0 {
				_ = <-ch_reportfiles
				go n.reportFiles(ch_reportfiles)
			}
			if len(ch_calctag) > 0 {
				_ = <-ch_calctag
				go n.serviceTag(ch_calctag)
			}
			if len(ch_replace) > 0 {
				_ = <-ch_replace
				go n.replaceIdle(ch_replace)
			}

		case <-task_Hour.C:
			n.connectBoot()
			if len(ch_resizespace) > 0 {
				_ = <-ch_resizespace
				go n.resizeSpace(ch_resizespace)
			}
		case <-ch_spaceMgt:
			go n.poisMgt(ch_spaceMgt)
		// case <-ch_restoreMgt:
		// 	go n.restoreMgt(ch_restoreMgt)
		case <-ch_discoverMgt:
			go n.discoverMgt(ch_discoverMgt)
		}
	}
}

func (n *Node) SetPublickey(pubkey []byte) error {
	rsaPubkey, err := x509.ParsePKCS1PublicKey(pubkey)
	if err != nil {
		return err
	}
	if n.key == nil {
		n.key = proof.NewKey()
	}
	n.key.Spk = rsaPubkey
	return nil
}

func (n *Node) SavePeer(peerid string, addr peer.AddrInfo) {
	if n.peerLock.TryLock() {
		n.peers[peerid] = addr
		n.peerLock.Unlock()
	}
}

func (n *Node) SaveOrUpdatePeerUnSafe(peerid string, addr peer.AddrInfo) {
	n.peers[peerid] = addr
}

func (n *Node) HasPeer(peerid string) bool {
	n.peerLock.RLock()
	_, ok := n.peers[peerid]
	n.peerLock.RUnlock()
	return ok
}

func (n *Node) GetPeer(peerid string) (peer.AddrInfo, bool) {
	n.peerLock.RLock()
	result, ok := n.peers[peerid]
	n.peerLock.RUnlock()
	return result, ok
}

func (n *Node) GetAllPeerIdString() []string {
	var result = make([]string, len(n.peers))
	n.peerLock.RLock()
	defer n.peerLock.RUnlock()
	var i int
	for k, _ := range n.peers {
		result[i] = k
		i++
	}
	return result
}

func (n *Node) GetAllPeerID() []peer.ID {
	var result = make([]peer.ID, len(n.peers))
	n.peerLock.RLock()
	defer n.peerLock.RUnlock()
	var i int
	for _, v := range n.peers {
		result[i] = v.ID
		i++
	}
	return result
}

func (n *Node) GetAllPeerIDMap() map[string]peer.AddrInfo {
	var result = make(map[string]peer.AddrInfo, len(n.peers))
	n.peerLock.RLock()
	defer n.peerLock.RUnlock()
	for k, v := range n.peers {
		result[k] = v
	}
	return result
}

func (n *Node) RemovePeerIntranetAddr() {
	n.peerLock.Lock()
	defer n.peerLock.Unlock()
	for k, v := range n.peers {
		var addrInfo peer.AddrInfo
		var addrs []multiaddr.Multiaddr
		for _, addr := range v.Addrs {
			if ipv4, ok := utils.FildIpv4([]byte(addr.String())); ok {
				if ok, err := utils.IsIntranetIpv4(ipv4); err == nil {
					if !ok {
						addrs = append(addrs, addr)
					}
				}
			}
		}
		if len(addrs) > 0 {
			addrInfo.ID = v.ID
			addrInfo.Addrs = utils.RemoveRepeatedAddr(addrs)
			n.SaveOrUpdatePeerUnSafe(v.ID.Pretty(), addrInfo)
		} else {
			delete(n.peers, k)
		}
	}
}

func (n *Node) SavePeersToDisk(path string) error {
	n.peerLock.RLock()
	buf, err := json.Marshal(n.peers)
	if err != nil {
		n.peerLock.RUnlock()
		return err
	}
	n.peerLock.RUnlock()
	err = sutils.WriteBufToFile(buf, n.peersPath)
	return err
}

func (n *Node) LoadPeersFromDisk(path string) error {
	buf, err := os.ReadFile(n.peersPath)
	if err != nil {
		return err
	}
	n.peerLock.Lock()
	err = json.Unmarshal(buf, &n.peers)
	n.peerLock.Unlock()
	return err
}

// tee peers

func (n *Node) SaveTeeWork(account string, peerid []byte) {
	n.teeLock.Lock()
	n.teeWorkers[account] = peerid
	n.teeLock.Unlock()
}

func (n *Node) GetTeeWork(account string) ([]byte, bool) {
	n.teeLock.RLock()
	result, ok := n.teeWorkers[account]
	n.teeLock.RUnlock()
	return result, ok
}

func (n *Node) GetAllTeeWorkAccount() []string {
	var result = make([]string, len(n.teeWorkers))
	n.teeLock.RLock()
	defer n.teeLock.RUnlock()
	var i int
	for k, _ := range n.teeWorkers {
		result[i] = k
		i++
	}
	return result
}

func (n *Node) GetAllTeeWorkPeerId() [][]byte {
	var result = make([][]byte, len(n.teeWorkers))
	n.teeLock.RLock()
	defer n.teeLock.RUnlock()
	var i int
	for _, v := range n.teeWorkers {
		result[i] = v
		i++
	}
	return result
}

func (n *Node) GetAllTeeWorkPeerIdString() []string {
	var result = make([]string, len(n.teeWorkers))
	n.teeLock.RLock()
	defer n.teeLock.RUnlock()
	var i int
	for _, v := range n.teeWorkers {
		result[i] = base58.Encode(v)
		i++
	}
	return result
}

func (n *Node) RebuildDirs() {
	os.RemoveAll(n.GetDirs().FileDir)
	os.RemoveAll(n.GetDirs().IdleDataDir)
	os.RemoveAll(n.GetDirs().IdleTagDir)
	os.RemoveAll(n.GetDirs().ProofDir)
	os.RemoveAll(n.GetDirs().ServiceTagDir)
	os.RemoveAll(n.GetDirs().TmpDir)
	os.RemoveAll(filepath.Join(n.Workspace(), configs.DbDir))
	os.RemoveAll(filepath.Join(n.Workspace(), configs.LogDir))
	os.MkdirAll(n.GetDirs().FileDir, pattern.DirMode)
	os.MkdirAll(n.GetDirs().TmpDir, pattern.DirMode)
	os.MkdirAll(n.GetDirs().IdleDataDir, pattern.DirMode)
	os.MkdirAll(n.GetDirs().IdleTagDir, pattern.DirMode)
	os.MkdirAll(n.GetDirs().ProofDir, pattern.DirMode)
	os.MkdirAll(n.GetDirs().ServiceTagDir, pattern.DirMode)
}
