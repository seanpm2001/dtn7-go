// SPDX-FileCopyrightText: 2019, 2021 Markus Sommer
// SPDX-FileCopyrightText: 2019, 2020, 2021 Alvar Penning
// SPDX-FileCopyrightText: 2021 Artur Sterz
// SPDX-FileCopyrightText: 2021 Jonas Höchst
//
// SPDX-License-Identifier: GPL-3.0-or-later

package mtcp

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/dtn7/cboring"

	"github.com/dtn7/dtn7-ng/pkg/bpv7"
	"github.com/dtn7/dtn7-ng/pkg/cla"
)

// MTCPClient is an implementation of a Minimal TCP Convergence-Layer client
// which connects to a MTCP server to send bundles. This struct implements
// a ConvergenceSender.
type MTCPClient struct {
	conn  net.Conn
	peer  bpv7.EndpointID
	mutex sync.Mutex

	permanent bool
	address   string

	stopSyn chan struct{}
	stopAck chan struct{}
}

// NewMTCPClient creates a new MTCPClient, connected to the given address for
// the registered endpoint ID. The permanent flag indicates if this MTCPClient
// should never be removed from the core.
func NewMTCPClient(address string, peer bpv7.EndpointID, permanent bool) *MTCPClient {
	return &MTCPClient{
		peer:      peer,
		permanent: permanent,
		address:   address,
	}
}

// NewAnonymousMTCPClient creates a new MTCPClient, connected to the given address.
// The permanent flag indicates if this MTCPClient should never be removed from
// the core.
func NewAnonymousMTCPClient(address string, permanent bool) *MTCPClient {
	return NewMTCPClient(address, bpv7.DtnNone(), permanent)
}

func (client *MTCPClient) Activate() (err error) {
	conn, connErr := dial(client.address)
	if connErr != nil {
		err = connErr
		return
	}

	client.stopSyn = make(chan struct{})
	client.stopAck = make(chan struct{})

	client.conn = conn

	go client.handler()
	return
}

func (client *MTCPClient) handler() {
	var ticker = time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Introduce ourselves once
	cla.GetManagerSingleton().NotifyConnect(client.peer)

	for {
		select {
		case <-client.stopSyn:
			_ = client.conn.Close()

			close(client.stopAck)

			return

		case <-ticker.C:
			client.mutex.Lock()
			err := cboring.WriteByteStringLen(0, client.conn)
			client.mutex.Unlock()

			if err != nil {
				log.WithFields(log.Fields{
					"client": client.String(),
					"error":  err,
				}).Error("MTCPClient: Keepalive erred")

				client.Close()
			}
		}
	}
}

func (client *MTCPClient) Send(bndl bpv7.Bundle) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("MTCPClient.Send: %v", r)
		}
	}()

	defer func() {
		if err != nil {
			client.Close()
		}
	}()

	client.mutex.Lock()
	defer client.mutex.Unlock()

	connWriter := bufio.NewWriter(client.conn)

	buff := new(bytes.Buffer)
	if cborErr := cboring.Marshal(&bndl, buff); cborErr != nil {
		err = cborErr
		return
	}

	if bsErr := cboring.WriteByteStringLen(uint64(buff.Len()), connWriter); bsErr != nil {
		err = bsErr
		return
	}

	if _, plErr := buff.WriteTo(connWriter); plErr != nil {
		err = plErr
		return
	}

	if flushErr := connWriter.Flush(); flushErr != nil {
		err = flushErr
		return
	}

	// Check if the connection is still alive with an empty, unbuffered packet
	if probeErr := cboring.WriteByteStringLen(0, client.conn); probeErr != nil {
		err = probeErr
		return
	}

	return
}

func (client *MTCPClient) Close() error {
	close(client.stopSyn)
	<-client.stopAck

	cla.GetManagerSingleton().NotifyDisconnect(client)

	return nil
}

func (client *MTCPClient) GetPeerEndpointID() bpv7.EndpointID {
	return client.peer
}

func (client *MTCPClient) Address() string {
	return client.address
}

func (client *MTCPClient) IsPermanent() bool {
	return client.permanent
}

func (client *MTCPClient) String() string {
	if client.conn != nil {
		return fmt.Sprintf("mtcp://%v", client.conn.RemoteAddr())
	} else {
		return fmt.Sprintf("mtcp://%s", client.address)
	}
}

func (client *MTCPClient) Active() bool {
	return true
}
