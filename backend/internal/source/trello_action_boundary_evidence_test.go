package source

import (
	"fmt"
	"testing"
)

func TestTrelloInitialActionPageCannotInventInclusiveBoundary(t *testing.T) {
	for _, phase := range []string{trelloPhaseBackfillActions, trelloPhaseCatchUpActions, trelloPhaseIncrementalActions} {
		for _, receiptCount := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/receipts=%d", phase, receiptCount), func(t *testing.T) {
				current, next, page := trelloCursorContractFixture(phase, "", receiptCount+1)
				next.Phase = trelloPhaseIncrementalCards
				if phase == trelloPhaseBackfillActions {
					next.Phase = trelloPhaseCatchUpActions
				}
				receipts := trelloActionReceipts(current.SourceID)
				if receiptCount > 0 {
					receipts = trelloActionReceipts(current.SourceID, 100)
					page.CursorAfter = receipts[0].ActionID
				}
				if err := validateTrelloPageCursorProgress(&current, &next, page, receipts, nil, false, "running"); err == nil {
					t.Fatal("initial action page accepted a record with no corresponding receipt or inclusive cursor boundary")
				}
				page.RecordCount--
				next.RecordsProcessed--
				if err := validateTrelloPageCursorProgress(&current, &next, page, receipts, nil, false, "running"); err != nil {
					t.Fatalf("exactly receipted initial page rejected: %v", err)
				}
			})
		}
	}
}
