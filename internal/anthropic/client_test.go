package anthropic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockClient_StreamsAllChunks(t *testing.T) {
	mock := &MockClient{}
	mock.Setup([]StreamChunk{
		{Text: "Hola"},
		{Text: " "},
		{Text: "mundo"},
		{IsDone: true, FinalText: "Hola mundo"},
	}, nil)

	chunks, errCh := mock.Stream(context.Background(), StreamRequest{})

	var got []StreamChunk
	for c := range chunks {
		got = append(got, c)
	}

	select {
	case err, ok := <-errCh:
		if ok {
			require.NoError(t, err)
		}
	default:
	}

	require.Len(t, got, 4)
	assert.Equal(t, "Hola", got[0].Text)
	assert.Equal(t, " ", got[1].Text)
	assert.Equal(t, "mundo", got[2].Text)
	assert.True(t, got[3].IsDone)
	assert.Equal(t, "Hola mundo", got[3].FinalText)
}

func TestMockClient_PropagatesError(t *testing.T) {
	wantErr := errors.New("upstream blew up")

	mock := &MockClient{}
	mock.Setup([]StreamChunk{
		{Text: "partial"},
	}, wantErr)

	chunks, errCh := mock.Stream(context.Background(), StreamRequest{})

	var got []StreamChunk
	for c := range chunks {
		got = append(got, c)
	}

	require.Len(t, got, 1)
	assert.Equal(t, "partial", got[0].Text)

	err, ok := <-errCh
	require.True(t, ok, "expected an error on the channel")
	assert.ErrorIs(t, err, wantErr)
}

func TestMockClient_RespectsContextCancellation(t *testing.T) {
	mock := &MockClient{}
	chunks := make([]StreamChunk, 1000)
	for i := range chunks {
		chunks[i] = StreamChunk{Text: "x"}
	}
	mock.Setup(chunks, nil)

	ctx, cancel := context.WithCancel(context.Background())
	chunksCh, errCh := mock.Stream(ctx, StreamRequest{})

	received := 0
	select {
	case <-chunksCh:
		received++
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range chunksCh {
		}
		for range errCh {
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("channels did not close after context cancel")
	}

	assert.Less(t, received, len(chunks), "should not have drained all chunks")
}
