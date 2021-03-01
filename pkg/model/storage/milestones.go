package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/gohornet/hornet/pkg/keymanager"
	"github.com/gohornet/hornet/pkg/model/hornet"
	"github.com/gohornet/hornet/pkg/model/milestone"
	iotago "github.com/iotaledger/iota.go/v2"
	"github.com/iotaledger/iota.go/v2/ed25519"
)

const (
	isNodeSyncedWithinThreshold = 2
)

type CoordinatorPublicKey struct {
	StartIndex milestone.Index
	EndIndex   milestone.Index
	PublicKey  ed25519.PublicKey
}

var (
	ErrInvalidMilestone = errors.New("invalid milestone")
)

func MilestoneCaller(handler interface{}, params ...interface{}) {
	handler.(func(cachedMs *CachedMilestone))(params[0].(*CachedMilestone).Retain())
}

func (s *Storage) ConfigureMilestones(cooKeyManager *keymanager.KeyManager, cooMilestonePublicKeyCount int) {
	s.keyManager = cooKeyManager
	s.milestonePublicKeyCount = cooMilestonePublicKeyCount
}

func (s *Storage) KeyManager() *keymanager.KeyManager {
	return s.keyManager
}

func (s *Storage) ResetMilestoneIndexes() {
	s.confirmedMilestoneLock.Lock()
	s.latestMilestoneLock.Lock()
	defer s.confirmedMilestoneLock.Unlock()
	defer s.latestMilestoneLock.Unlock()

	s.confirmedMilestoneIndex = 0
	s.latestMilestoneIndex = 0
}

// GetMilestoneOrNil returns the CachedMessage of a milestone index or nil if it doesn't exist.
// message +1
func (s *Storage) GetMilestoneCachedMessageOrNil(milestoneIndex milestone.Index) *CachedMessage {

	cachedMs := s.GetCachedMilestoneOrNil(milestoneIndex) // milestone +1
	if cachedMs == nil {
		return nil
	}
	defer cachedMs.Release(true) // milestone -1

	return s.GetCachedMessageOrNil(cachedMs.GetMilestone().MessageID)
}

// IsNodeSynced returns whether the node is synced.
func (s *Storage) IsNodeSynced() bool {
	return s.isNodeSynced
}

// IsNodeSyncedWithThreshold returns whether the node is synced within a certain threshold.
func (s *Storage) IsNodeSyncedWithThreshold() bool {
	return s.isNodeSyncedThreshold
}

// WaitForNodeSynced waits at most "timeout" duration for the node to become fully sync.
// if it is not at least synced within threshold, it will return false immediately.
// this is used to avoid small glitches of IsNodeSynced when the sync state is important,
// but a new milestone came in lately.
func (s *Storage) WaitForNodeSynced(timeout time.Duration) bool {

	if !s.isNodeSyncedThreshold {
		// node is not even synced within threshold, and therefore it is unsync
		return false
	}

	if s.isNodeSynced {
		// node is synced, no need to wait
		return true
	}

	// create a channel that gets closed if the node got synced
	s.waitForNodeSyncedChannelsLock.Lock()
	waitForNodeSyncedChan := make(chan struct{})
	s.waitForNodeSyncedChannels = append(s.waitForNodeSyncedChannels, waitForNodeSyncedChan)
	s.waitForNodeSyncedChannelsLock.Unlock()

	// check again after the channel was created
	if s.isNodeSynced {
		// node is synced, no need to wait
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// we wait either until the node got synced or we reached the deadline
	select {
	case <-waitForNodeSyncedChan:
	case <-ctx.Done():
	}

	return s.isNodeSynced
}

// The node is synced if LMI != 0 and CMI == LMI.
func (s *Storage) updateNodeSynced(confirmedIndex, latestIndex milestone.Index) {
	if latestIndex == 0 {
		s.isNodeSynced = false
		s.isNodeSyncedThreshold = false
		return
	}

	s.isNodeSynced = confirmedIndex == latestIndex
	if s.isNodeSynced {
		// if the node is sync, signal all waiting routines at the end
		defer func() {
			s.waitForNodeSyncedChannelsLock.Lock()
			defer s.waitForNodeSyncedChannelsLock.Unlock()

			// signal all routines that are waiting
			for _, channel := range s.waitForNodeSyncedChannels {
				close(channel)
			}

			// create an empty slice for new signals
			s.waitForNodeSyncedChannels = make([]chan struct{}, 0)
		}()
	}

	// catch overflow
	if latestIndex < isNodeSyncedWithinThreshold {
		s.isNodeSyncedThreshold = true
		return
	}

	s.isNodeSyncedThreshold = confirmedIndex >= (latestIndex - isNodeSyncedWithinThreshold)
}

// SetConfirmedMilestoneIndex sets the confirmed milestone index.
func (s *Storage) SetConfirmedMilestoneIndex(index milestone.Index, updateSynced ...bool) {
	s.confirmedMilestoneLock.Lock()
	if s.confirmedMilestoneIndex > index {
		panic(fmt.Sprintf("current confirmed milestone (%d) is newer than (%d)", s.confirmedMilestoneIndex, index))
	}
	s.confirmedMilestoneIndex = index
	s.confirmedMilestoneLock.Unlock()

	if len(updateSynced) > 0 && !updateSynced[0] {
		// always call updateNodeSynced if parameter is not given.
		return
	}

	s.updateNodeSynced(index, s.GetLatestMilestoneIndex())
}

// OverwriteConfirmedMilestoneIndex is used to set older confirmed milestones (revalidation).
func (s *Storage) OverwriteConfirmedMilestoneIndex(index milestone.Index) {
	s.confirmedMilestoneLock.Lock()
	s.confirmedMilestoneIndex = index
	s.confirmedMilestoneLock.Unlock()

	if s.isNodeSynced {
		s.updateNodeSynced(index, s.GetLatestMilestoneIndex())
	}
}

// GetConfirmedMilestoneIndex returns the confirmed milestone index.
func (s *Storage) GetConfirmedMilestoneIndex() milestone.Index {
	s.confirmedMilestoneLock.RLock()
	defer s.confirmedMilestoneLock.RUnlock()

	return s.confirmedMilestoneIndex
}

// SetLatestMilestoneIndex sets the latest milestone index.
func (s *Storage) SetLatestMilestoneIndex(index milestone.Index, updateSynced ...bool) bool {

	s.latestMilestoneLock.Lock()

	if s.latestMilestoneIndex >= index {
		// current LMI is bigger than new LMI => abort
		s.latestMilestoneLock.Unlock()
		return false
	}

	s.latestMilestoneIndex = index
	s.latestMilestoneLock.Unlock()

	if len(updateSynced) > 0 && !updateSynced[0] {
		// always call updateNodeSynced if parameter is not given
		return true
	}

	s.updateNodeSynced(s.GetConfirmedMilestoneIndex(), index)

	return true
}

// GetLatestMilestoneIndex returns the latest milestone index.
func (s *Storage) GetLatestMilestoneIndex() milestone.Index {
	s.latestMilestoneLock.RLock()
	defer s.latestMilestoneLock.RUnlock()

	return s.latestMilestoneIndex
}

// message +1
func (s *Storage) FindClosestNextMilestoneOrNil(index milestone.Index) *CachedMilestone {
	lmi := s.GetLatestMilestoneIndex()
	if lmi == 0 {
		// no milestone received yet, check the next 100 milestones as a workaround
		lmi = s.GetConfirmedMilestoneIndex() + 100
	}

	if index == 4294967295 {
		// prevent overflow (2**32 -1)
		return nil
	}

	for {
		index++

		if index > lmi {
			return nil
		}

		cachedMs := s.GetCachedMilestoneOrNil(index) // milestone +1
		if cachedMs != nil {
			return cachedMs
		}
	}
}

// VerifyMilestone checks if the message contains a valid milestone payload.
// Returns a milestone payload if the signature is valid.
func (s *Storage) VerifyMilestone(message *Message) *iotago.Milestone {
	ms := message.GetMilestone()
	if ms == nil {
		return nil
	}

	for idx, parent := range message.message.Parents {
		if parent != ms.Parents[idx] {
			// parents in message and payload have to be equal
			return nil
		}
	}

	if err := ms.VerifySignatures(s.milestonePublicKeyCount, s.keyManager.GetPublicKeysSetForMilestoneIndex(milestone.Index(ms.Index))); err != nil {
		return nil
	}

	return ms
}

// StoreMilestone stores the milestone in the storage layer and triggers the ReceivedValidMilestone event.
func (s *Storage) StoreMilestone(messageID hornet.MessageID, ms *iotago.Milestone) {

	cachedMilestone := s.storeMilestone(milestone.Index(ms.Index), messageID, time.Unix(int64(ms.Timestamp), 0))

	// Force release to store milestones without caching
	defer cachedMilestone.Release(true) // milestone +-0

	s.Events.ReceivedValidMilestone.Trigger(cachedMilestone) // milestone pass +1
}
