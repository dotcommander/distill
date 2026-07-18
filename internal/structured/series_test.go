package structured

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectRecordSeries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		source         string
		wantNil        bool
		wantConfidence float64
		wantContains   []string
	}{
		{
			name: "three complete sensor records",
			source: strings.Join([]string{
				"Sensor #1",
				"Temp: 70F",
				"Humidity: 40%",
				"Sensor #2",
				"Temp: 71F",
				"Humidity: 41%",
				"Sensor #3",
				"Temp: 72F",
				"Humidity: 42%",
			}, "\n"),
			wantConfidence: 1.0,
			wantContains: []string{
				"| # | Temp | Humidity |",
				"| 1 | 70F | 40% |",
				"| 2 | 71F | 41% |",
				"| 3 | 72F | 42% |",
			},
		},
		{
			name: "two records below minimum",
			source: strings.Join([]string{
				"Sensor #1",
				"Temp: 70F",
				"Humidity: 40%",
				"Sensor #2",
				"Temp: 71F",
				"Humidity: 41%",
			}, "\n"),
			wantNil: true,
		},
		{
			name: "modal columns tolerate one missing field",
			source: strings.Join([]string{
				"Sensor #1",
				"Temp: 70F",
				"Humidity: 40%",
				"Sensor #2",
				"Temp: 71F",
				"Humidity: 41%",
				"Sensor #3",
				"Temp: 72F",
				"Humidity: 42%",
				"Sensor #4",
				"Temp: 73F",
			}, "\n"),
			wantConfidence: 0.75,
			wantContains: []string{
				"| # | Temp | Humidity |",
				"| 4 | 73F |  |",
			},
		},
		{
			name: "inconsistent keys below confidence threshold",
			source: strings.Join([]string{
				"Sensor #1",
				"Temp: 70F",
				"Sensor #2",
				"Humidity: 41%",
				"Sensor #3",
				"Pressure: 30",
				"Sensor #4",
				"Voltage: 3.3",
				"Sensor #5",
				"Current: 1.2",
			}, "\n"),
			wantNil: true,
		},
		{
			name: "chapter prose without fields",
			source: strings.Join([]string{
				"Chapter 1",
				"Once upon a time.",
				"Chapter 2",
				"More prose.",
				"Chapter 3",
				"The end.",
			}, "\n"),
			wantNil: true,
		},
		{
			name: "pipes are escaped in values",
			source: strings.Join([]string{
				"Sensor #1",
				"Temp: 70|71",
				"Sensor #2",
				"Temp: 72",
				"Sensor #3",
				"Temp: 73",
			}, "\n"),
			wantConfidence: 1.0,
			wantContains: []string{
				"| 1 | 70\\|71 |",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blocks := detectRecordSeries(tt.source)
			if tt.wantNil {
				require.Nil(t, blocks)
				return
			}

			require.Len(t, blocks, 1)
			block := blocks[0]
			assert.Equal(t, RecordSeries, block.Kind)
			assert.Equal(t, "Sensor", block.Title)
			assert.Equal(t, tt.wantConfidence, block.Confidence)
			assert.Equal(t, 0, block.SrcStart)
			for _, want := range tt.wantContains {
				assert.Contains(t, block.Markdown, want)
			}
		})
	}
}
