package routing

import (
	"reflect"
	"testing"
)

func TestSplitShares(t *testing.T) {
	for _, tc := range []struct {
		name   string
		panels []uint
		stored map[uint]int
		want   []PanelShare
	}{
		{"stored weights", []uint{1, 2}, map[uint]int{1: 60, 2: 40}, []PanelShare{{1, 60}, {2, 40}}},
		{"weights scale to 100", []uint{1, 2}, map[uint]int{1: 3, 2: 1}, []PanelShare{{1, 75}, {2, 25}}},
		{"no weights split evenly", []uint{1, 2, 3}, nil, []PanelShare{{1, 34}, {2, 33}, {3, 33}}},
		{"a new panel resets to even", []uint{1, 2}, map[uint]int{1: 100}, []PanelShare{{1, 50}, {2, 50}}},
		{"single panel", []uint{7}, map[uint]int{7: 20}, []PanelShare{{7, 100}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := splitShares(tc.panels, tc.stored); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitShares() = %v, want %v", got, tc.want)
			}
		})
	}
}
