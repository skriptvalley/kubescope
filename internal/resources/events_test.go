package resources

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestShapeEventsSortNewestFirst(t *testing.T) {
	t0 := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	events := []corev1.Event{
		{
			Type: "Normal", Reason: "Scheduled", Message: "assigned",
			Count: 1, LastTimestamp: metav1.NewTime(t0),
		},
		{
			Type: "Warning", Reason: "BackOff", Message: "back-off restarting",
			Count: 5, LastTimestamp: metav1.NewTime(t0.Add(2 * time.Minute)),
		},
		{
			Type: "Normal", Reason: "Pulled", Message: "image pulled",
			Count: 1, LastTimestamp: metav1.NewTime(t0.Add(1 * time.Minute)),
		},
	}
	got := shapeEvents(events)
	require.Len(t, got, 3)
	assert.Equal(t, "BackOff", got[0].Reason, "newest first")
	assert.Equal(t, "Pulled", got[1].Reason)
	assert.Equal(t, "Scheduled", got[2].Reason)
	assert.Equal(t, int32(5), got[0].Count)
	assert.Equal(t, "Warning", got[0].Type)
	assert.Equal(t, "2026-07-15T10:02:00Z", got[0].LastSeen)
}

func TestShapeEventsEmpty(t *testing.T) {
	assert.Empty(t, shapeEvents(nil))
	assert.NotNil(t, shapeEvents([]corev1.Event{}), "empty slice, not nil, for a clean JSON array")
}

func TestEventLastSeenFallbacks(t *testing.T) {
	t0 := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

	// Series LastObservedTime wins when present.
	withSeries := corev1.Event{
		LastTimestamp: metav1.NewTime(t0),
		Series:        &corev1.EventSeries{Count: 9, LastObservedTime: metav1.NewMicroTime(t0.Add(time.Hour))},
	}
	assert.Equal(t, t0.Add(time.Hour), eventLastSeen(&withSeries).Time)
	assert.Equal(t, int32(9), eventCount(&withSeries))

	// Falls back to eventTime, then firstTimestamp, then creation.
	onlyEventTime := corev1.Event{EventTime: metav1.NewMicroTime(t0)}
	assert.Equal(t, t0, eventLastSeen(&onlyEventTime).Time)

	onlyFirst := corev1.Event{FirstTimestamp: metav1.NewTime(t0)}
	assert.Equal(t, t0, eventLastSeen(&onlyFirst).Time)

	onlyCreation := corev1.Event{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(t0)}}
	assert.Equal(t, t0, eventLastSeen(&onlyCreation).Time)

	// Count defaults to 1 when neither series nor legacy count is set.
	assert.Equal(t, int32(1), eventCount(&corev1.Event{}))
}
