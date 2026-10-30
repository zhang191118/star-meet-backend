package service

import (
	"testing"

	"star-meet-backend/model/database"
)

func TestAdvancePlotStateFollowsAllowedProgression(t *testing.T) {
	state := PlotStateOpening
	for _, step := range []struct {
		trigger string
		want    string
	}{
		{PlotTriggerInteraction, PlotStateAcquaintance},
		{PlotTriggerDisclosure, PlotStateAmbiguous},
		{PlotTriggerConflict, PlotStateTension},
		{PlotTriggerReconciliation, PlotStateRepair},
		{PlotTriggerPromise, PlotStateCommitted},
	} {
		state = advancePlotState(state, step.trigger)
		if state != step.want {
			t.Fatalf("trigger %q moved plot to %q, want %q", step.trigger, state, step.want)
		}
	}
}

func TestAdvancePlotStateDoesNotJumpAcrossUnmetBeats(t *testing.T) {
	if got := advancePlotState(PlotStateOpening, PlotTriggerPromise); got != PlotStateOpening {
		t.Fatalf("promise should not skip opening beat, got %q", got)
	}
	if got := advancePlotState(PlotStateTension, PlotTriggerInteraction); got != PlotStateTension {
		t.Fatalf("generic interaction should not resolve tension, got %q", got)
	}
	if got := advancePlotState(PlotStateCommitted, PlotTriggerPause); got != PlotStateCommitted {
		t.Fatalf("pause should preserve committed state, got %q", got)
	}
}

func TestPlotSnapshotAdvancesBeatAndKeepsOpenThreads(t *testing.T) {
	snapshot := PlotSnapshot{State: PlotStateTension, Beat: 3, OpenThreads: []string{"未解释的离开原因"}}
	next := advancePlotSnapshot(snapshot, PlotTriggerReconciliation, "误会已经说开")
	if next.State != PlotStateRepair || next.Beat != 4 {
		t.Fatalf("unexpected plot snapshot: %+v", next)
	}
	if len(next.OpenThreads) != 2 || next.OpenThreads[0] != "误会已经说开" || next.OpenThreads[1] != "未解释的离开原因" {
		t.Fatalf("expected new and existing open threads to be tracked, got %+v", next.OpenThreads)
	}
}

func TestPlotSnapshotPersistsOnSession(t *testing.T) {
	db := setupChatStreamMetadataTestDB(t)
	if err := db.Create(&database.Session{SessionID: "plot-session", UserID: "plot-user", CharacterID: "plot-char"}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	service := &DirectorService{DB: db}
	snapshot := PlotSnapshot{State: PlotStateRepair, Beat: 4, OpenThreads: []string{"周末复习约定"}}
	if err := service.SavePlotSnapshot("plot-session", snapshot); err != nil {
		t.Fatalf("save plot snapshot: %v", err)
	}
	var session database.Session
	if err := db.Where("session_id = ?", "plot-session").First(&session).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if session.PlotState != PlotStateRepair || session.PlotBeat != 4 || session.PlotThreads == "" {
		t.Fatalf("unexpected persisted plot snapshot: %+v", session)
	}
}
