package processing

import (
	"sync"

	"github.com/dtn7/dtn7-ng/pkg/bpv7"
	"github.com/dtn7/dtn7-ng/pkg/cla"
	"github.com/dtn7/dtn7-ng/pkg/routing"
	"github.com/dtn7/dtn7-ng/pkg/store"
	log "github.com/sirupsen/logrus"
)

var NodeID bpv7.EndpointID

// forwardingAsync implements the bundle forwarding procedure described in RFC9171 section 5.4
func forwardingAsync(bundleDescriptor *store.BundleDescriptor) {
	log.WithField("bundle", bundleDescriptor.ID.String()).Debug("Processing bundle")

	// Step 1: add "Forward Pending, remove "Dispatch Pending"
	err := bundleDescriptor.AddConstraint(store.ForwardPending)
	if err != nil {
		log.WithFields(log.Fields{
			"bundle": bundleDescriptor.ID,
			"error":  err,
		}).Error("Error adding constraint to bundle")
		return
	}
	err = bundleDescriptor.RemoveConstraint(store.DispatchPending)
	if err != nil {
		log.WithFields(log.Fields{
			"bundle": bundleDescriptor.ID,
			"error":  err,
		}).Error("Error removing constraint from bundle")
		return
	}

	// Step 2: determine if contraindicated - whatever that means
	// Step 2.1: Call routing algorithm(?)
	forwardToPeers := routing.GetAlgorithmSingleton().SelectPeersForForwarding(bundleDescriptor)

	// Step 3: if contraindicated, call `contraindicateBundle`, and return
	if len(forwardToPeers) == 0 {
		bundleContraindicated(bundleDescriptor)
		return
	}

	// Step 4:
	bundle, err := bundleDescriptor.Load()
	if err != nil {
		log.WithFields(log.Fields{
			"bundle": bundleDescriptor.ID,
			"error":  err,
		}).Error("Error loading bundle from disk")
		return
	}
	// Step 4.1: remove previous node block
	if prevNodeBlock, err := bundle.ExtensionBlock(bpv7.ExtBlockTypePreviousNodeBlock); err == nil {
		bundle.RemoveExtensionBlockByBlockNumber(prevNodeBlock.BlockNumber)
	}
	// Step 4.2: add new previous node block
	prevNodeBlock := bpv7.NewCanonicalBlock(0, 0, bpv7.NewPreviousNodeBlock(NodeID))
	err = bundle.AddExtensionBlock(prevNodeBlock)
	if err != nil {
		log.WithFields(log.Fields{
			"bundle": bundleDescriptor.ID,
			"error":  err,
		}).Error("Error adding PreviousNodeBlock to bundle")
	}
	// TODO: Step 4.3: update bundle age block
	// Step 4.4: call CLAs for transmission
	forwardBundle(bundleDescriptor, forwardToPeers)

	// Step 6: remove "Forward Pending"
	err = bundleDescriptor.RemoveConstraint(store.ForwardPending)
	if err != nil {
		log.WithFields(log.Fields{
			"bundle": bundleDescriptor.ID,
			"error":  err,
		}).Error("Error removing constraint from bundle")
		return
	}
}

func BundleForwarding(bundleDescriptor *store.BundleDescriptor) {
	go forwardingAsync(bundleDescriptor)
}

func bundleContraindicated(bundleDescriptor *store.BundleDescriptor) {
	// TODO: is there anything else to do here?
	err := bundleDescriptor.ResetConstraints()
	if err != nil {
		log.WithFields(log.Fields{
			"bundle": bundleDescriptor.ID,
			"error":  err,
		}).Error("Error resetting bundle constraints")
	}
}

func forwardBundle(bundleDescriptor *store.BundleDescriptor, peers []cla.ConvergenceSender) {
	bundle, err := bundleDescriptor.Load()
	if err != nil {
		log.WithFields(log.Fields{
			"bundle": bundleDescriptor.ID,
			"error":  err,
		}).Error("Failed to load bundle from disk")
		return
	}

	// Step 1: spawn a new goroutine for each cla
	sentAtLeastOnce := false
	successfulSends := make([]bool, len(peers))

	var wg sync.WaitGroup
	var once sync.Once

	wg.Add(len(peers))
	log.WithField("bundle", bundleDescriptor.ID.String()).Debug("Initialising sending")
	for i, peer := range peers {
		go func(peer cla.ConvergenceSender, i int) {
			log.WithFields(log.Fields{
				"bundle": bundleDescriptor.ID,
				"cla":    peer,
			}).Info("Sending bundle to a CLA (ConvergenceSender)")

			if err := peer.Send(bundle); err != nil {
				log.WithFields(log.Fields{
					"bundle": bundleDescriptor.ID,
					"cla":    peer,
					"error":  err,
				}).Warn("Sending bundle failed")
			} else {
				log.WithFields(log.Fields{
					"bundle": bundleDescriptor.ID,
					"cla":    peer,
				}).Debug("Sending bundle succeeded")

				successfulSends[i] = true

				once.Do(func() { sentAtLeastOnce = true })
			}

			wg.Done()
		}(peer, i)
	}
	wg.Wait()
	log.WithField("bundle", bundleDescriptor.ID.String()).Debug("Sending finished")

	// Step 2 track which sends were successful
	for i, success := range successfulSends {
		if success {
			log.WithFields(log.Fields{
				"bundle": bundleDescriptor.ID.String(),
				"cla":    peers[i].GetPeerEndpointID(),
			}).Debug("Successfully sent to peer")
			bundleDescriptor.AddAlreadySent(peers[i].GetPeerEndpointID())
		}
	}

	if sentAtLeastOnce {
		log.WithField("bundle", bundleDescriptor.ID.String()).Debug("Bundle successfully sent")
	}
}

func DispatchPending() {
	log.Debug("Dispatching bundles")

	bndls, err := store.GetStoreSingleton().GetDispatchable()
	if err != nil {
		log.WithError(err).Error("Error dispatching pending bundles")
		return
	}
	log.WithField("bundles", bndls).Debug("Bundles to dispatch")

	for _, bndl := range bndls {
		BundleForwarding(bndl)
	}
}
