package participation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gohornet/hornet/pkg/model/milestone"
	"github.com/gohornet/hornet/pkg/model/participation"
	"github.com/gohornet/hornet/pkg/model/participation/test"
	"github.com/gohornet/hornet/pkg/model/storage"
)

func TestEventStateHelpers(t *testing.T) {

	eventCommenceIndex := milestone.Index(90)
	eventStartIndex := milestone.Index(100)
	eventEndIndex := milestone.Index(200)

	eventBuilder := participation.NewEventBuilder("Test", eventCommenceIndex, eventStartIndex, eventEndIndex, "Sample")

	questionBuilder := participation.NewQuestionBuilder("Q1", "-")
	questionBuilder.AddAnswer(&participation.Answer{
		Value:          1,
		Text:           "A1",
		AdditionalInfo: "-",
	})
	questionBuilder.AddAnswer(&participation.Answer{
		Value:          2,
		Text:           "A2",
		AdditionalInfo: "-",
	})

	question, err := questionBuilder.Build()
	require.NoError(t, err)

	ballotBuilder := participation.NewBallotBuilder()
	ballotBuilder.AddQuestion(question)
	payload, err := ballotBuilder.Build()
	require.NoError(t, err)

	eventBuilder.Payload(payload)

	event, err := eventBuilder.Build()
	require.NoError(t, err)

	// Verify status
	require.Equal(t, "upcoming", event.Status(89))
	require.Equal(t, "commencing", event.Status(90))
	require.Equal(t, "commencing", event.Status(91))
	require.Equal(t, "holding", event.Status(100))
	require.Equal(t, "holding", event.Status(101))
	require.Equal(t, "holding", event.Status(199))
	require.Equal(t, "ended", event.Status(200))
	require.Equal(t, "ended", event.Status(201))

	// Verify IsAcceptingParticipation
	require.False(t, event.IsAcceptingParticipation(89))
	require.True(t, event.IsAcceptingParticipation(90))
	require.True(t, event.IsAcceptingParticipation(91))
	require.True(t, event.IsAcceptingParticipation(99))
	require.True(t, event.IsAcceptingParticipation(100))
	require.True(t, event.IsAcceptingParticipation(101))
	require.True(t, event.IsAcceptingParticipation(199))
	require.False(t, event.IsAcceptingParticipation(200))
	require.False(t, event.IsAcceptingParticipation(201))

	// Verify IsCountingParticipation
	require.False(t, event.IsCountingParticipation(89))
	require.False(t, event.IsCountingParticipation(90))
	require.False(t, event.IsCountingParticipation(91))
	require.False(t, event.IsCountingParticipation(99))
	require.True(t, event.IsCountingParticipation(100))
	require.True(t, event.IsCountingParticipation(101))
	require.True(t, event.IsCountingParticipation(199))
	require.False(t, event.IsCountingParticipation(200))
	require.False(t, event.IsCountingParticipation(201))

	// Verify ShouldAcceptParticipation
	require.False(t, event.ShouldAcceptParticipation(89))
	require.False(t, event.ShouldAcceptParticipation(90))
	require.True(t, event.ShouldAcceptParticipation(91))
	require.True(t, event.ShouldAcceptParticipation(99))
	require.True(t, event.ShouldAcceptParticipation(100))
	require.True(t, event.ShouldAcceptParticipation(101))
	require.True(t, event.ShouldAcceptParticipation(199))
	require.True(t, event.ShouldAcceptParticipation(200))
	require.False(t, event.ShouldAcceptParticipation(201))

	// Verify ShouldCountParticipation
	require.False(t, event.ShouldCountParticipation(89))
	require.False(t, event.ShouldCountParticipation(90))
	require.False(t, event.ShouldCountParticipation(91))
	require.False(t, event.ShouldCountParticipation(99))
	require.False(t, event.ShouldCountParticipation(100))
	require.True(t, event.ShouldCountParticipation(101))
	require.True(t, event.ShouldCountParticipation(199))
	require.True(t, event.ShouldCountParticipation(200))
	require.False(t, event.ShouldCountParticipation(201))
}

func TestEventStates(t *testing.T) {
	env := test.NewParticipationTestEnv(t, 1_000_000, 150_000_000, 200_000_000, 300_000_000, false)
	defer env.Cleanup()

	confirmedMilestoneIndex := env.ConfirmedMilestoneIndex() // 4
	require.Equal(t, milestone.Index(4), confirmedMilestoneIndex)

	require.Empty(t, env.ParticipationManager().Events())
	eventID := env.RegisterDefaultEvent(5, 1, 2)

	event := env.ParticipationManager().Event(eventID)
	require.NotNil(t, event)

	// Verify the configured participation indexes
	require.Equal(t, milestone.Index(5), event.CommenceMilestoneIndex())
	require.Equal(t, milestone.Index(6), event.StartMilestoneIndex())
	require.Equal(t, milestone.Index(8), event.EndMilestoneIndex())

	// No participation should be running right now
	require.Equal(t, 1, len(env.ParticipationManager().Events()))
	env.AssertEventsCount(0, 0)

	env.IssueMilestone() // 5

	// Event should be accepting votes, but not counting
	require.Equal(t, 1, len(env.ParticipationManager().Events()))
	env.AssertEventsCount(1, 0)

	env.IssueMilestone() // 6

	// Event should be accepting and counting votes
	require.Equal(t, 1, len(env.ParticipationManager().Events()))
	env.AssertEventsCount(1, 1)

	env.IssueMilestone() // 7

	// Event should be accepting and counting votes
	require.Equal(t, 1, len(env.ParticipationManager().Events()))
	env.AssertEventsCount(1, 1)

	env.IssueMilestone() // 8

	// Event should be ended
	require.Equal(t, 1, len(env.ParticipationManager().Events()))
	env.AssertEventsCount(0, 0)
}

func TestSingleBallotVote(t *testing.T) {
	env := test.NewParticipationTestEnv(t, 1_000_000, 150_000_000, 200_000_000, 300_000_000, false)
	defer env.Cleanup()

	confirmedMilestoneIndex := env.ConfirmedMilestoneIndex() // 4
	require.Equal(t, milestone.Index(4), confirmedMilestoneIndex)

	eventID := env.RegisterDefaultEvent(5, 2, 3)

	event := env.ParticipationManager().Event(eventID)
	require.NotNil(t, event)

	// Verify the configured participation indexes
	require.Equal(t, milestone.Index(5), event.CommenceMilestoneIndex())
	require.Equal(t, milestone.Index(7), event.StartMilestoneIndex())
	require.Equal(t, milestone.Index(10), event.EndMilestoneIndex())

	// Event should not be accepting votes yet
	require.Equal(t, 0, len(env.ParticipationManager().EventsAcceptingParticipation()))

	// Issue a vote and milestone
	env.IssueDefaultBallotVoteAndMilestone(eventID, env.Wallet1) // 5

	// Participations should not have been counted so far because it was not accepting votes yet
	status, err := env.ParticipationManager().EventStatus(eventID)
	require.NoError(t, err)
	env.PrintJSON(status)
	require.Equal(t, env.ConfirmedMilestoneIndex(), status.MilestoneIndex)
	env.AssertDefaultBallotAnswerStatus(eventID, 0, 0)

	// Event should be accepting votes now
	require.Equal(t, 1, len(env.ParticipationManager().EventsAcceptingParticipation()))

	// Participation again
	castVote := env.IssueDefaultBallotVoteAndMilestone(eventID, env.Wallet1) // 6

	// Event should be accepting votes, but the vote should not be weighted yet, just added to the current status
	env.AssertEventsCount(1, 0)

	status, err = env.ParticipationManager().EventStatus(eventID)
	require.NoError(t, err)
	env.PrintJSON(status)
	require.Equal(t, env.ConfirmedMilestoneIndex(), status.MilestoneIndex)
	env.AssertDefaultBallotAnswerStatus(eventID, 1_000, 0)

	env.IssueMilestone() // 7

	// Event should be accepting and counting votes, but the vote we did before should not be weighted yet
	env.AssertEventsCount(1, 1)

	status, err = env.ParticipationManager().EventStatus(eventID)
	require.NoError(t, err)
	env.PrintJSON(status)
	require.Equal(t, env.ConfirmedMilestoneIndex(), status.MilestoneIndex)
	env.AssertDefaultBallotAnswerStatus(eventID, 1_000, 0)

	env.IssueMilestone() // 8

	// Event should be accepting and counting votes, the vote should now be weighted
	env.AssertEventsCount(1, 1)

	status, err = env.ParticipationManager().EventStatus(eventID)
	require.NoError(t, err)
	env.PrintJSON(status)
	require.Equal(t, env.ConfirmedMilestoneIndex(), status.MilestoneIndex)
	env.AssertDefaultBallotAnswerStatus(eventID, 1_000, 1_000)

	env.IssueMilestone() // 9

	// Event should be accepting and counting votes, the vote should now be weighted double
	env.AssertEventsCount(1, 1)

	status, err = env.ParticipationManager().EventStatus(eventID)
	require.NoError(t, err)
	env.PrintJSON(status)
	require.Equal(t, env.ConfirmedMilestoneIndex(), status.MilestoneIndex)
	env.AssertDefaultBallotAnswerStatus(eventID, 1_000, 2_000)

	env.IssueMilestone() // 10

	// Event should be ended
	env.AssertEventsCount(0, 0)
	env.AssertDefaultBallotAnswerStatus(eventID, 1_000, 3_000)

	env.IssueMilestone() // 11

	var trackedVote *participation.TrackedParticipation
	trackedVote, err = env.ParticipationManager().ParticipationForOutputID(eventID, castVote.Message().GeneratedUTXO().OutputID())
	require.NoError(t, err)
	require.Equal(t, castVote.Message().StoredMessageID(), trackedVote.MessageID)
	require.Equal(t, milestone.Index(6), trackedVote.StartIndex)
	require.Equal(t, milestone.Index(10), trackedVote.EndIndex)

	var messageFromParticipationStore *storage.Message
	messageFromParticipationStore, err = env.ParticipationManager().MessageForMessageID(trackedVote.MessageID)
	require.NoError(t, err)
	require.NotNil(t, messageFromParticipationStore)
	require.Equal(t, messageFromParticipationStore.Message(), castVote.Message().IotaMessage())
}

func TestBallotVoteCancel(t *testing.T) {
	env := test.NewParticipationTestEnv(t, 1_000_000, 150_000_000, 200_000_000, 300_000_000, false)
	defer env.Cleanup()

	confirmedMilestoneIndex := env.ConfirmedMilestoneIndex() // 4
	require.Equal(t, milestone.Index(4), confirmedMilestoneIndex)

	eventID := env.RegisterDefaultEvent(5, 2, 5)

	event := env.ParticipationManager().Event(eventID)
	require.NotNil(t, event)

	// Verify the configured participation indexes
	require.Equal(t, milestone.Index(5), event.CommenceMilestoneIndex())
	require.Equal(t, milestone.Index(7), event.StartMilestoneIndex())
	require.Equal(t, milestone.Index(12), event.EndMilestoneIndex())

	// Event should not be accepting votes yet
	require.Equal(t, 0, len(env.ParticipationManager().EventsAcceptingParticipation()))

	env.IssueMilestone() // 5

	// Issue a vote and milestone
	castVote1 := env.IssueDefaultBallotVoteAndMilestone(eventID, env.Wallet1) // 6

	// Verify vote
	env.AssertDefaultBallotAnswerStatus(eventID, 1_000, 0)

	// Cancel vote
	cancelVote1Msg := env.CancelParticipations(env.Wallet1)
	env.IssueMilestone(cancelVote1Msg.StoredMessageID(), env.LastMilestoneMessageID()) // 7

	// Verify vote
	env.AssertDefaultBallotAnswerStatus(eventID, 0, 0)

	// Participation again
	castVote2 := env.IssueDefaultBallotVoteAndMilestone(eventID, env.Wallet1) // 8

	// Verify vote
	env.AssertDefaultBallotAnswerStatus(eventID, 1_000, 1_000)

	// Cancel vote
	cancelVote2Msg := env.CancelParticipations(env.Wallet1)
	env.IssueMilestone(cancelVote2Msg.StoredMessageID(), env.LastMilestoneMessageID()) // 9

	// Verify vote
	env.AssertDefaultBallotAnswerStatus(eventID, 0, 1_000)

	env.IssueMilestone() // 10

	// Verify vote
	env.AssertDefaultBallotAnswerStatus(eventID, 0, 1_000)

	// Participation again
	castVote3 := env.IssueDefaultBallotVoteAndMilestone(eventID, env.Wallet1) // 11

	// Verify vote
	env.AssertDefaultBallotAnswerStatus(eventID, 1_000, 2_000)

	env.AssertEventParticipationStatus(eventID, 1, 2)

	// Verify the last issued vote is still active, i.e. EndIndex == 0
	env.AssertTrackedParticipation(eventID, castVote3, 11, 0, 1_000_000)

	// Issue final milestone that ends the participation
	env.IssueMilestone() // 12

	// There should be no active votes left
	env.AssertEventParticipationStatus(eventID, 0, 3)
	env.AssertDefaultBallotAnswerStatus(eventID, 1_000, 3_000)

	// Verify the vote history after the participation ended
	env.AssertTrackedParticipation(eventID, castVote1, 6, 7, 1_000_000)
	env.AssertTrackedParticipation(eventID, castVote2, 8, 9, 1_000_000)
	env.AssertTrackedParticipation(eventID, castVote3, 11, 12, 1_000_000)
}

func TestBallotAddVoteBalanceBySweeping(t *testing.T) {
	env := test.NewParticipationTestEnv(t, 5_000_000, 150_000_000, 200_000_000, 300_000_000, false)
	defer env.Cleanup()

	confirmedMilestoneIndex := env.ConfirmedMilestoneIndex() // 4
	require.Equal(t, milestone.Index(4), confirmedMilestoneIndex)

	eventID := env.RegisterDefaultEvent(5, 2, 5)

	event := env.ParticipationManager().Event(eventID)
	require.NotNil(t, event)

	// Verify the configured participation indexes
	require.Equal(t, milestone.Index(5), event.CommenceMilestoneIndex())
	require.Equal(t, milestone.Index(7), event.StartMilestoneIndex())
	require.Equal(t, milestone.Index(12), event.EndMilestoneIndex())

	env.IssueMilestone() // 5
	env.IssueMilestone() // 6
	env.IssueMilestone() // 7

	// Issue a vote and milestone
	castVote1 := env.IssueDefaultBallotVoteAndMilestone(eventID, env.Wallet1, 5_000_000) // 8
	require.NotNil(t, castVote1)

	// Verify current vote status
	env.AssertEventParticipationStatus(eventID, 1, 0)
	env.AssertDefaultBallotAnswerStatus(eventID, 5_000, 5_000)

	// Send more funds to wallet1
	transfer := env.Transfer(env.Wallet2, env.Wallet1, 1_500_000)
	require.Equal(t, 2, len(env.Wallet1.Outputs()))

	env.IssueMilestone(transfer.StoredMessageID()) // 9

	// Verify current vote status
	env.AssertEventParticipationStatus(eventID, 1, 0)
	env.AssertDefaultBallotAnswerStatus(eventID, 5_000, 10_000)

	// Sweep all funds
	require.Equal(t, 2, len(env.Wallet1.Outputs()))
	castVote2 := env.IssueDefaultBallotVoteAndMilestone(eventID, env.Wallet1, 6_500_000) // 10
	require.Equal(t, 1, len(env.Wallet1.Outputs()))

	// Verify current vote status
	env.AssertEventParticipationStatus(eventID, 1, 1)
	env.AssertDefaultBallotAnswerStatus(eventID, 6_500, 16_500)

	// Verify both votes
	env.AssertTrackedParticipation(eventID, castVote1, 8, 10, 5_000_000)
	env.AssertTrackedParticipation(eventID, castVote2, 10, 0, 6_500_000)
}

func TestBallotAddVoteBalanceByMultipleOutputs(t *testing.T) {
	env := test.NewParticipationTestEnv(t, 5_000_000, 150_000_000, 200_000_000, 300_000_000, false)
	defer env.Cleanup()

	confirmedMilestoneIndex := env.ConfirmedMilestoneIndex() // 4
	require.Equal(t, milestone.Index(4), confirmedMilestoneIndex)

	eventID := env.RegisterDefaultEvent(5, 2, 5)

	event := env.ParticipationManager().Event(eventID)
	require.NotNil(t, event)

	// Verify the configured participation indexes
	require.Equal(t, milestone.Index(5), event.CommenceMilestoneIndex())
	require.Equal(t, milestone.Index(7), event.StartMilestoneIndex())
	require.Equal(t, milestone.Index(12), event.EndMilestoneIndex())

	env.IssueMilestone() // 5
	env.IssueMilestone() // 6
	env.IssueMilestone() // 7

	// Issue a vote and milestone
	castVote1 := env.IssueDefaultBallotVoteAndMilestone(eventID, env.Wallet1) // 8
	require.NotNil(t, castVote1)

	// Verify current vote status
	env.AssertEventParticipationStatus(eventID, 1, 0)
	env.AssertDefaultBallotAnswerStatus(eventID, 5_000, 5_000)

	// Send more funds to wallet1
	transfer := env.Transfer(env.Wallet2, env.Wallet1, 1_500_000)
	require.Equal(t, 2, len(env.Wallet1.Outputs()))

	env.IssueMilestone(transfer.StoredMessageID()) // 9

	// Verify current vote status
	env.AssertEventParticipationStatus(eventID, 1, 0)
	env.AssertDefaultBallotAnswerStatus(eventID, 5_000, 10_000)

	// Send a separate vote, without sweeping
	require.Equal(t, 2, len(env.Wallet1.Outputs()))
	castVote2 := env.NewParticipationHelper(env.Wallet1).
		Amount(1_500_000).
		UsingOutput(transfer.GeneratedUTXO()).
		AddDefaultBallotVote(eventID).
		Send()
	env.IssueMilestone(castVote2.Message().StoredMessageID()) // 10
	require.Equal(t, 2, len(env.Wallet1.Outputs()))

	// Verify current vote status
	env.AssertEventParticipationStatus(eventID, 2, 0)
	env.AssertDefaultBallotAnswerStatus(eventID, 6_500, 16_500)

	// Verify both votes
	env.AssertTrackedParticipation(eventID, castVote1, 8, 0, 5_000_000)
	env.AssertTrackedParticipation(eventID, castVote2, 10, 0, 1_500_000)
}

func TestMultipleBallotVotes(t *testing.T) {
	env := test.NewParticipationTestEnv(t, 5_000_000, 150_000_000, 200_000_000, 300_000_000, false)
	defer env.Cleanup()

	confirmedMilestoneIndex := env.ConfirmedMilestoneIndex() // 4
	require.Equal(t, milestone.Index(4), confirmedMilestoneIndex)

	eventID := env.RegisterDefaultEvent(5, 2, 5)

	event := env.ParticipationManager().Event(eventID)
	require.NotNil(t, event)

	// Verify the configured participation indexes
	require.Equal(t, milestone.Index(5), event.CommenceMilestoneIndex())
	require.Equal(t, milestone.Index(7), event.StartMilestoneIndex())
	require.Equal(t, milestone.Index(12), event.EndMilestoneIndex())

	env.IssueMilestone() // 5
	env.IssueMilestone() // 6
	env.IssueMilestone() // 7

	wallet1Vote := env.NewParticipationHelper(env.Wallet1).
		WholeWalletBalance().
		AddParticipation(&participation.Participation{
			EventID: eventID,
			Answers: []byte{0},
		}).
		Send()

	wallet2Vote := env.NewParticipationHelper(env.Wallet2).
		WholeWalletBalance().
		AddParticipation(&participation.Participation{
			EventID: eventID,
			Answers: []byte{10},
		}).
		Send()

	wallet3Vote := env.NewParticipationHelper(env.Wallet3).
		WholeWalletBalance().
		AddParticipation(&participation.Participation{
			EventID: eventID,
			Answers: []byte{20},
		}).
		Send()

	_, confStats := env.IssueMilestone(wallet1Vote.Message().StoredMessageID(), wallet2Vote.Message().StoredMessageID(), wallet3Vote.Message().StoredMessageID()) // 8
	require.Equal(t, 3+1, confStats.MessagesReferenced)                                                                                                           // 3 + milestone itself
	require.Equal(t, 3, confStats.MessagesIncludedWithTransactions)
	require.Equal(t, 0, confStats.MessagesExcludedWithConflictingTransactions)
	require.Equal(t, 1, confStats.MessagesExcludedWithoutTransactions) // the milestone

	// Verify current vote status
	env.AssertEventParticipationStatus(eventID, 3, 0)
	env.AssertBallotAnswerStatus(eventID, 5_000, 5_000, 0, 0)
	env.AssertBallotAnswerStatus(eventID, 150_000, 150_000, 0, 10)
	env.AssertBallotAnswerStatus(eventID, 200_000, 200_000, 0, 20)

	// Verify all votes
	env.AssertTrackedParticipation(eventID, wallet1Vote, 8, 0, 5_000_000)
	env.AssertTrackedParticipation(eventID, wallet2Vote, 8, 0, 150_000_000)
	env.AssertTrackedParticipation(eventID, wallet3Vote, 8, 0, 200_000_000)

	env.IssueMilestone() // 9
	env.IssueMilestone() // 10
	env.IssueMilestone() // 11
	env.IssueMilestone() // 12

	// Verify current vote status
	env.AssertEventParticipationStatus(eventID, 0, 3)
	env.AssertBallotAnswerStatus(eventID, 5_000, 25_000, 0, 0)
	env.AssertBallotAnswerStatus(eventID, 150_000, 750_000, 0, 10)
	env.AssertBallotAnswerStatus(eventID, 200_000, 1000_000, 0, 20)

	// Verify all votes
	env.AssertTrackedParticipation(eventID, wallet1Vote, 8, 12, 5_000_000)
	env.AssertTrackedParticipation(eventID, wallet2Vote, 8, 12, 150_000_000)
	env.AssertTrackedParticipation(eventID, wallet3Vote, 8, 12, 200_000_000)
}

func TestChangeOpinionMidVote(t *testing.T) {
	env := test.NewParticipationTestEnv(t, 5_000_000, 150_000_000, 200_000_000, 300_000_000, false)
	defer env.Cleanup()

	confirmedMilestoneIndex := env.ConfirmedMilestoneIndex() // 4
	require.Equal(t, milestone.Index(4), confirmedMilestoneIndex)

	eventID := env.RegisterDefaultEvent(5, 2, 5)

	event := env.ParticipationManager().Event(eventID)
	require.NotNil(t, event)

	// Verify the configured indexes
	require.Equal(t, milestone.Index(5), event.CommenceMilestoneIndex())
	require.Equal(t, milestone.Index(7), event.StartMilestoneIndex())
	require.Equal(t, milestone.Index(12), event.EndMilestoneIndex())

	env.IssueMilestone() // 5
	env.IssueMilestone() // 6
	env.IssueMilestone() // 7

	wallet1Vote1 := env.NewParticipationHelper(env.Wallet1).
		WholeWalletBalance().
		AddParticipation(&participation.Participation{
			EventID: eventID,
			Answers: []byte{10},
		}).
		Send()

	env.IssueMilestone(wallet1Vote1.Message().StoredMessageID()) // 8

	// Verify current vote status
	env.AssertEventParticipationStatus(eventID, 1, 0)
	env.AssertBallotAnswerStatus(eventID, 5_000, 5_000, 0, 10)

	// Verify all votes
	env.AssertTrackedParticipation(eventID, wallet1Vote1, 8, 0, 5_000_000)

	// Change opinion

	wallet1Vote2 := env.NewParticipationHelper(env.Wallet1).
		WholeWalletBalance().
		AddParticipation(&participation.Participation{
			EventID: eventID,
			Answers: []byte{20},
		}).
		Send()

	env.IssueMilestone(wallet1Vote2.Message().StoredMessageID()) // 9

	// Verify current vote status
	env.AssertEventParticipationStatus(eventID, 1, 1)
	env.AssertBallotAnswerStatus(eventID, 0, 5_000, 0, 10)
	env.AssertBallotAnswerStatus(eventID, 5_000, 5_000, 0, 20)

	// Verify all votes
	env.AssertTrackedParticipation(eventID, wallet1Vote1, 8, 9, 5_000_000)
	env.AssertTrackedParticipation(eventID, wallet1Vote2, 9, 0, 5_000_000)

	// Cancel vote
	cancel := env.CancelParticipations(env.Wallet1)

	env.IssueMilestone(cancel.StoredMessageID()) // 10

	// Verify current vote status
	env.AssertEventParticipationStatus(eventID, 0, 2)
	env.AssertBallotAnswerStatus(eventID, 0, 5_000, 0, 10)
	env.AssertBallotAnswerStatus(eventID, 0, 5_000, 0, 20)

	// Verify all votes
	env.AssertTrackedParticipation(eventID, wallet1Vote1, 8, 9, 5_000_000)
	env.AssertTrackedParticipation(eventID, wallet1Vote2, 9, 10, 5_000_000)
}

func TestMultipleConcurrentEventsWithBallot(t *testing.T) {
	env := test.NewParticipationTestEnv(t, 5_000_000, 150_000_000, 200_000_000, 300_000_000, false)
	defer env.Cleanup()

	confirmedMilestoneIndex := env.ConfirmedMilestoneIndex() // 4
	require.Equal(t, milestone.Index(4), confirmedMilestoneIndex)

	eventID1 := env.RegisterDefaultEvent(5, 2, 5)
	eventID2 := env.RegisterDefaultEvent(7, 2, 5)

	event1 := env.ParticipationManager().Event(eventID1)
	require.NotNil(t, event1)

	event2 := env.ParticipationManager().Event(eventID2)
	require.NotNil(t, event1)

	// Verify the configured indexes
	require.Equal(t, milestone.Index(5), event1.CommenceMilestoneIndex())
	require.Equal(t, milestone.Index(7), event1.StartMilestoneIndex())
	require.Equal(t, milestone.Index(12), event1.EndMilestoneIndex())

	require.Equal(t, milestone.Index(7), event2.CommenceMilestoneIndex())
	require.Equal(t, milestone.Index(9), event2.StartMilestoneIndex())
	require.Equal(t, milestone.Index(14), event2.EndMilestoneIndex())

	env.IssueMilestone() // 5

	env.AssertEventsCount(1, 0)
	env.AssertEventParticipationStatus(eventID1, 0, 0)
	env.AssertEventParticipationStatus(eventID2, 0, 0)

	wallet1Vote1 := env.NewParticipationHelper(env.Wallet1).
		WholeWalletBalance().
		// Participation for the commencing event1
		AddParticipation(&participation.Participation{
			EventID: eventID1,
			Answers: []byte{10},
		}).
		// Participation too early for the upcoming event2
		AddParticipation(&participation.Participation{
			EventID: eventID2,
			Answers: []byte{20},
		}).
		Send()

	wallet2Vote1 := env.NewParticipationHelper(env.Wallet2).
		WholeWalletBalance().
		// Participation for the commencing event1
		AddParticipation(&participation.Participation{
			EventID: eventID1,
			Answers: []byte{10},
		}).
		Send()

	env.IssueMilestone(wallet1Vote1.Message().StoredMessageID(), wallet2Vote1.Message().StoredMessageID()) // 6

	env.AssertEventsCount(1, 0)
	env.AssertEventParticipationStatus(eventID1, 2, 0)
	env.AssertEventParticipationStatus(eventID2, 0, 0)

	env.IssueMilestone() // 7

	env.AssertEventsCount(2, 1)
	env.AssertEventParticipationStatus(eventID1, 2, 0)
	env.AssertEventParticipationStatus(eventID2, 0, 0)

	wallet3Vote1 := env.NewParticipationHelper(env.Wallet3).
		WholeWalletBalance().
		// Participation for the commencing event2
		AddParticipation(&participation.Participation{
			EventID: eventID2,
			Answers: []byte{20},
		}).
		Send()

	env.IssueMilestone(wallet3Vote1.Message().StoredMessageID()) // 8

	env.AssertEventsCount(2, 1)
	env.AssertEventParticipationStatus(eventID1, 2, 0)
	env.AssertEventParticipationStatus(eventID2, 1, 0)

	env.IssueMilestone() // 9

	env.AssertEventsCount(2, 2)
	env.AssertEventParticipationStatus(eventID1, 2, 0)
	env.AssertEventParticipationStatus(eventID2, 1, 0)

	env.IssueMilestone() // 10

	wallet1Vote2 := env.NewParticipationHelper(env.Wallet1).
		WholeWalletBalance().
		// Keep Participation for the holding event1
		AddParticipation(&participation.Participation{
			EventID: eventID1,
			Answers: []byte{10},
		}).
		// Re-Participation holding event2
		AddParticipation(&participation.Participation{
			EventID: eventID2,
			Answers: []byte{20},
		}).
		Send()

	env.IssueMilestone(wallet1Vote2.Message().StoredMessageID()) // 11

	env.AssertEventsCount(2, 2)
	env.AssertEventParticipationStatus(eventID1, 2, 1)
	env.AssertEventParticipationStatus(eventID2, 2, 0)

	wallet4Vote1 := env.NewParticipationHelper(env.Wallet4).
		WholeWalletBalance().
		// Participation for the holding event1
		AddParticipation(&participation.Participation{
			EventID: eventID1,
			Answers: []byte{0},
		}).
		Send()

	env.IssueMilestone(wallet4Vote1.Message().StoredMessageID()) // 12

	env.AssertEventsCount(1, 1)
	env.AssertEventParticipationStatus(eventID1, 0, 4)
	env.AssertEventParticipationStatus(eventID2, 2, 0)

	wallet4Vote2 := env.NewParticipationHelper(env.Wallet4).
		WholeWalletBalance().
		// Participation for the holding event2
		AddParticipation(&participation.Participation{
			EventID: eventID2,
			Answers: []byte{20},
		}).
		Send()

	env.IssueMilestone(wallet4Vote2.Message().StoredMessageID()) // 13

	env.AssertEventsCount(1, 1)
	env.AssertEventParticipationStatus(eventID1, 0, 4)
	env.AssertEventParticipationStatus(eventID2, 3, 0)

	env.IssueMilestone() // 14

	env.AssertEventsCount(0, 0)
	env.AssertEventParticipationStatus(eventID1, 0, 4)
	env.AssertEventParticipationStatus(eventID2, 0, 3)

	// Verify all votes
	env.AssertTrackedParticipation(eventID1, wallet1Vote1, 6, 11, 5_000_000) // Voted 1
	env.AssertInvalidParticipation(eventID2, wallet1Vote1)
	env.AssertTrackedParticipation(eventID1, wallet1Vote2, 11, 12, 5_000_000)   // Voted 1
	env.AssertTrackedParticipation(eventID2, wallet1Vote2, 11, 14, 5_000_000)   // Voted 2
	env.AssertTrackedParticipation(eventID1, wallet2Vote1, 6, 12, 150_000_000)  // Voted 1
	env.AssertTrackedParticipation(eventID2, wallet3Vote1, 8, 14, 200_000_000)  // Voted 2
	env.AssertTrackedParticipation(eventID1, wallet4Vote1, 12, 12, 300_000_000) // Voted 0
	env.AssertTrackedParticipation(eventID2, wallet4Vote2, 13, 14, 300_000_000) // Voted 2

	// Verify end results
	env.AssertBallotAnswerStatus(eventID1, 300_000, 300_000, 0, 0)
	env.AssertBallotAnswerStatus(eventID1, 155_000, 775_000, 0, 10)
	env.AssertBallotAnswerStatus(eventID1, 0, 0, 0, 20)

	env.AssertBallotAnswerStatus(eventID2, 0, 0, 0, 0)
	env.AssertBallotAnswerStatus(eventID2, 0, 0, 0, 10)
	env.AssertBallotAnswerStatus(eventID2, 505_000, 1_620_000, 0, 20)
}

func TestStakingRewards(t *testing.T) {
	env := test.NewParticipationTestEnv(t, 5_000_000, 1_587_529, 5_589_977, 300_000_000, false)
	defer env.Cleanup()

	confirmedMilestoneIndex := env.ConfirmedMilestoneIndex() // 4
	require.Equal(t, milestone.Index(4), confirmedMilestoneIndex)

	eventBuilder := participation.NewEventBuilder("AlbinoPugCoin", 5, 7, 12, "The first DogCoin on the Tangle")
	eventBuilder.Payload(&participation.Staking{
		Text:           "The rarest DogCoin on earth",
		Symbol:         "APUG",
		Numerator:      25,
		Denominator:    100,
		AdditionalInfo: "Have you seen an albino Pug?",
	})

	event, err := eventBuilder.Build()
	require.NoError(t, err)

	eventID, err := env.ParticipationManager().StoreEvent(event)
	require.NoError(t, err)

	// Verify the configured indexes
	require.Equal(t, milestone.Index(5), event.CommenceMilestoneIndex())
	require.Equal(t, milestone.Index(7), event.StartMilestoneIndex())
	require.Equal(t, milestone.Index(12), event.EndMilestoneIndex())

	env.IssueMilestone() // 5
	env.AssertEventsCount(1, 0)

	stakeWallet1 := env.NewParticipationHelper(env.Wallet1).
		WholeWalletBalance().
		AddParticipation(&participation.Participation{
			EventID: eventID,
			Answers: []byte{},
		}).
		Send()

	stakeWallet2 := env.NewParticipationHelper(env.Wallet2).
		WholeWalletBalance().
		AddParticipation(&participation.Participation{
			EventID: eventID,
			Answers: []byte{},
		}).
		Send()

	stakeWallet3 := env.NewParticipationHelper(env.Wallet3).
		WholeWalletBalance().
		AddParticipation(&participation.Participation{
			EventID: eventID,
			Answers: []byte{},
		}).
		Send()

	env.IssueMilestone(stakeWallet1.Message().StoredMessageID(), stakeWallet2.Message().StoredMessageID(), stakeWallet3.Message().StoredMessageID()) // 6
	env.AssertEventsCount(1, 0)
	env.AssertRewardBalance(eventID, env.Wallet1.Address(), 0)
	env.AssertRewardBalance(eventID, env.Wallet2.Address(), 0)
	env.AssertRewardBalance(eventID, env.Wallet3.Address(), 0)

	env.IssueMilestone() // 7
	env.AssertEventsCount(1, 1)
	env.AssertRewardBalance(eventID, env.Wallet1.Address(), 0)
	env.AssertRewardBalance(eventID, env.Wallet2.Address(), 0)
	env.AssertRewardBalance(eventID, env.Wallet3.Address(), 0)

	env.IssueMilestone() // 8
	env.AssertRewardBalance(eventID, env.Wallet1.Address(), 1_250_000)
	env.AssertRewardBalance(eventID, env.Wallet2.Address(), 396_882)
	env.AssertRewardBalance(eventID, env.Wallet3.Address(), 1_397_494)

	env.IssueMilestone() // 9
	env.AssertRewardBalance(eventID, env.Wallet1.Address(), 2_500_000)
	env.AssertRewardBalance(eventID, env.Wallet2.Address(), 793_764)
	env.AssertRewardBalance(eventID, env.Wallet3.Address(), 2_794_988)

	env.IssueMilestone() // 10
	env.AssertRewardBalance(eventID, env.Wallet1.Address(), 3_750_000)
	env.AssertRewardBalance(eventID, env.Wallet2.Address(), 1_190_646)
	env.AssertRewardBalance(eventID, env.Wallet3.Address(), 4_192_482)

	env.IssueMilestone() // 11
	env.AssertRewardBalance(eventID, env.Wallet1.Address(), 5_000_000)
	env.AssertRewardBalance(eventID, env.Wallet2.Address(), 1_587_528)
	env.AssertRewardBalance(eventID, env.Wallet3.Address(), 5_589_976)

	env.IssueMilestone() // 12
	env.AssertRewardBalance(eventID, env.Wallet1.Address(), 6_250_000)
	env.AssertRewardBalance(eventID, env.Wallet2.Address(), 1_984_410)
	env.AssertRewardBalance(eventID, env.Wallet3.Address(), 6_987_470)

	env.IssueMilestone() // 13
	env.AssertRewardBalance(eventID, env.Wallet1.Address(), 6_250_000)
	env.AssertRewardBalance(eventID, env.Wallet2.Address(), 1_984_410)
	env.AssertRewardBalance(eventID, env.Wallet3.Address(), 6_987_470)

}