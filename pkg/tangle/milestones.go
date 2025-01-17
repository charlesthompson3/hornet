package tangle

import (
	"github.com/gohornet/hornet/pkg/model/hornet"
	"github.com/gohornet/hornet/pkg/model/storage"
)

func (t *Tangle) processValidMilestone(messageID hornet.MessageID, cachedMilestone *storage.CachedMilestone, requested bool) {
	defer cachedMilestone.Release(true) // milestone -1

	t.Events.ReceivedNewMilestoneMessage.Trigger(messageID)

	confirmedMsIndex := t.syncManager.ConfirmedMilestoneIndex()
	msIndex := cachedMilestone.Milestone().Index()

	if t.syncManager.SetLatestMilestoneIndex(msIndex) {
		t.Events.LatestMilestoneChanged.Trigger(cachedMilestone) // milestone pass +1
		t.Events.LatestMilestoneIndexChanged.Trigger(msIndex)
	}
	t.milestoneSolidifierWorkerPool.TrySubmit(msIndex, false)

	if msIndex > confirmedMsIndex {
		t.LogInfof("Valid milestone detected! Index: %d", msIndex)
		t.requester.RequestMilestoneParents(cachedMilestone.Retain()) // milestone pass +1
	} else if requested {
		pruningIndex := t.storage.SnapshotInfo().PruningIndex
		if msIndex < pruningIndex {
			// this should not happen. we requested a milestone that is below pruning index
			t.LogPanicf("Synced too far back! Index: %d, PruningIndex: %d", msIndex, pruningIndex)
		}
	}
}
