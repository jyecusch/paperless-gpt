package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// peakTrackingClient records the most GetDocument calls in flight at once,
// holding each one briefly so concurrent calls overlap.
type peakTrackingClient struct {
	*recordingClient
	mu       sync.Mutex
	inFlight int
	peak     int
}

func (c *peakTrackingClient) GetDocument(ctx context.Context, documentID int) (Document, error) {
	c.mu.Lock()
	c.inFlight++
	c.peak = max(c.peak, c.inFlight)
	c.mu.Unlock()

	time.Sleep(50 * time.Millisecond)

	c.mu.Lock()
	c.inFlight--
	c.mu.Unlock()
	return c.recordingClient.GetDocument(ctx, documentID)
}

func TestProcessAutoTagDocuments_Workers(t *testing.T) {
	prevAutoTag, prevMaxRetries := autoTag, autoTagMaxRetries
	t.Cleanup(func() { autoTag, autoTagMaxRetries = prevAutoTag, prevMaxRetries })
	autoTag = "paperless-gpt-auto"
	autoTagMaxRetries = 0

	documents := []Document{
		{ID: 1, Tags: []string{autoTag}},
		{ID: 2, Tags: []string{autoTag}},
		{ID: 3, Tags: []string{autoTag}},
		{ID: 4, Tags: []string{autoTag}},
	}
	newClient := func() *peakTrackingClient {
		return &peakTrackingClient{recordingClient: &recordingClient{
			taggedDocuments: map[string][]Document{autoTag: documents},
			// Suggestions fail fast after GetDocument; only the overlap matters.
			getAllTagsErr: errors.New("no LLM in this test"),
		}}
	}

	for _, tc := range []struct {
		workers  string
		wantPeak int
	}{
		{workers: "", wantPeak: 1},
		{workers: "2", wantPeak: 2},
		{workers: "not a number", wantPeak: 1},
	} {
		t.Run("AUTO_TAG_WORKERS="+tc.workers, func(t *testing.T) {
			t.Setenv("AUTO_TAG_WORKERS", tc.workers)
			client := newClient()
			app := &App{Client: client}

			_, err := app.processAutoTagDocuments(context.Background())

			assert.Error(t, err, "every document's suggestions fail")
			assert.Equal(t, tc.wantPeak, client.peak, "documents processed at once")
		})
	}
}
