package source

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

func TestTrelloPreparedFullPagesSatisfyDurableCheckpointContract(t *testing.T) {
	for _, phase := range []string{trelloPhaseBackfillCards, trelloPhaseBackfillActions} {
		for _, inclusive := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/inclusive=%t", phase, inclusive), func(t *testing.T) {
				const firstID = 20_000
				before := firstID + 1
				if inclusive {
					before = firstID
				}
				count := trelloCardPageSize
				if phase == trelloPhaseBackfillActions {
					count = trelloActionPageSize
				}
				cards := make([]trelloCard, count)
				actions := make([]trelloAction, count)
				for index := 0; index < count; index++ {
					cards[index] = trelloCard{ID: testTrelloMongoID(firstID - index), Name: "Checkpoint fixture", IDList: "list-1", DateLastActivity: "2026-09-25T12:00:00Z"}
					actions[index] = trelloAction{ID: cards[index].ID, Type: "commentCard", Date: cards[index].DateLastActivity, Data: trelloActionData{Card: trelloActionCard{ID: testTrelloMongoID(42)}, Text: "Checkpoint fixture"}}
				}
				newTrelloDurablePageServer(t,
					func(request *http.Request) []trelloCard {
						if phase != trelloPhaseBackfillCards {
							t.Error("action slice requested card inventory")
						}
						if request.URL.Query().Get("before") != testTrelloMongoID(before) {
							t.Error("provider card cursor was not the durable cursor")
						}
						return cards
					},
					func(request *http.Request) []trelloAction {
						if phase != trelloPhaseBackfillActions {
							t.Error("card slice requested action inventory")
						}
						if request.URL.Query().Get("before") != testTrelloMongoID(before) {
							t.Error("provider action cursor was not the durable cursor")
						}
						return actions
					},
				)
				source := newTrelloSource(uuid.New(), "abc123XY", "")
				job := &models.SourceSyncJob{ID: uuid.New(), SourceID: source.ID, Status: "running"}
				state := &models.TrelloSyncState{SourceID: source.ID, OwnerIdentity: source.OwnerIdentity, BoardID: source.SyncTarget, LogicalJobID: trelloUUIDPointer(job.ID), Generation: 1, Phase: phase, CycleStartedAt: time.Now().UTC()}
				if phase == trelloPhaseBackfillCards {
					state.CardCursor = testTrelloMongoID(before)
				} else {
					state.ActionCursor = testTrelloMongoID(before)
				}
				repository := &trelloSliceFixtureRepo{fakeSourceRepo: newFakeSourceRepo(source), state: state}
				slice, err := NewService(repository, nil).(*service).prepareTrelloSyncSlice(context.Background(), source, job)
				if err != nil {
					t.Fatal(err)
				}
				if err := validateTrelloPageCursorProgress(&slice.Current, &slice.Next, slice.Page, slice.Receipts, slice.SeenCardExternalIDs, slice.Complete, job.Status); err != nil {
					t.Fatalf("prepared provider page cannot be durably checkpointed: %v", err)
				}
				if slice.Complete || slice.Next.Phase != phase || slice.Page.RecordCount != count || slice.Page.CursorAfter != testTrelloMongoID(firstID-count+1) || slice.FinalCursor != "" {
					t.Fatal("full provider page lost its continuation or implied full-board completion")
				}
				if phase == trelloPhaseBackfillActions {
					wantReceipts := count
					if inclusive {
						wantReceipts--
					}
					if len(slice.Receipts) != wantReceipts || len(slice.Items) != wantReceipts {
						t.Fatal("inclusive boundary was duplicated or a new action lost")
					}
				}
			})
		}
	}
}
