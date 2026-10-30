package service

import (
	"encoding/json"
	"strings"

	"star-meet-backend/model/database"
)

const (
	PlotStateOpening      = "opening"
	PlotStateAcquaintance = "acquaintance"
	PlotStateAmbiguous    = "ambiguous"
	PlotStateTension      = "tension"
	PlotStateRepair       = "repair"
	PlotStateCommitted    = "committed"

	PlotTriggerInteraction    = "interaction"
	PlotTriggerDisclosure     = "disclosure"
	PlotTriggerConflict       = "conflict"
	PlotTriggerReconciliation = "reconciliation"
	PlotTriggerPromise        = "promise"
	PlotTriggerPause          = "pause"
)

type PlotSnapshot struct {
	State       string   `json:"state"`
	Beat        int      `json:"beat"`
	OpenThreads []string `json:"open_threads,omitempty"`
}

func advancePlotState(state, trigger string) string {
	if state == "" {
		state = PlotStateOpening
	}
	if trigger == PlotTriggerPause {
		return state
	}
	switch state {
	case PlotStateOpening:
		if trigger == PlotTriggerInteraction {
			return PlotStateAcquaintance
		}
	case PlotStateAcquaintance:
		if trigger == PlotTriggerDisclosure {
			return PlotStateAmbiguous
		}
	case PlotStateAmbiguous:
		if trigger == PlotTriggerConflict {
			return PlotStateTension
		}
	case PlotStateTension:
		if trigger == PlotTriggerReconciliation {
			return PlotStateRepair
		}
	case PlotStateRepair:
		if trigger == PlotTriggerPromise {
			return PlotStateCommitted
		}
	case PlotStateCommitted:
		if trigger == PlotTriggerConflict {
			return PlotStateTension
		}
	}
	return state
}

func advancePlotSnapshot(snapshot PlotSnapshot, trigger, thread string) PlotSnapshot {
	if snapshot.State == "" {
		snapshot.State = PlotStateOpening
	}
	nextState := advancePlotState(snapshot.State, trigger)
	if nextState != snapshot.State {
		snapshot.Beat++
	}
	snapshot.State = nextState
	if thread = strings.TrimSpace(thread); thread != "" {
		threads := make([]string, 0, len(snapshot.OpenThreads)+1)
		threads = append(threads, thread)
		for _, existing := range snapshot.OpenThreads {
			if strings.TrimSpace(existing) != "" && existing != thread {
				threads = append(threads, existing)
			}
		}
		if len(threads) > 5 {
			threads = threads[:5]
		}
		snapshot.OpenThreads = threads
	}
	return snapshot
}

func (d *DirectorService) SavePlotSnapshot(sessionID string, snapshot PlotSnapshot) error {
	if snapshot.State == "" {
		snapshot.State = PlotStateOpening
	}
	threads, err := json.Marshal(snapshot.OpenThreads)
	if err != nil {
		return err
	}
	return d.DB.Model(&database.Session{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]interface{}{
			"plot_state":   snapshot.State,
			"plot_beat":    snapshot.Beat,
			"plot_threads": string(threads),
		}).Error
}

func plotSnapshotFromSession(session database.Session) PlotSnapshot {
	snapshot := PlotSnapshot{State: session.PlotState, Beat: session.PlotBeat}
	if snapshot.State == "" {
		snapshot.State = PlotStateOpening
	}
	if strings.TrimSpace(session.PlotThreads) != "" {
		_ = json.Unmarshal([]byte(session.PlotThreads), &snapshot.OpenThreads)
	}
	return snapshot
}
