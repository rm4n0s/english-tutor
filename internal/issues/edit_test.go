package issues

import "testing"

func TestRangeRebase(t *testing.T) {
	r := Range{10, 15} // the issue covers units 10..14
	tests := []struct {
		name   string
		edit   Edit
		want   Range
		wantOK bool
	}{
		{"insert far before shifts forward", Edit{Pos: 2, Removed: 0, Added: 3}, Range{13, 18}, true},
		{"delete far before shifts back", Edit{Pos: 2, Removed: 4, Added: 0}, Range{6, 11}, true},
		{"replace far before with the same length", Edit{Pos: 0, Removed: 3, Added: 3}, r, true},
		{"replace far before with a longer text", Edit{Pos: 0, Removed: 3, Added: 8}, Range{15, 20}, true},
		{"edit ending one unit before the start is far", Edit{Pos: 6, Removed: 3, Added: 1}, Range{8, 13}, true},
		{"insert one unit before the start is far", Edit{Pos: 9, Removed: 0, Added: 2}, Range{12, 17}, true},
		{"insert exactly at the start touches", Edit{Pos: 10, Removed: 0, Added: 1}, Range{}, false},
		{"edit ending exactly at the start touches", Edit{Pos: 7, Removed: 3, Added: 0}, Range{}, false},
		{"insert inside touches", Edit{Pos: 12, Removed: 0, Added: 1}, Range{}, false},
		{"delete inside touches", Edit{Pos: 11, Removed: 2, Added: 0}, Range{}, false},
		{"replace the whole range touches", Edit{Pos: 10, Removed: 5, Added: 5}, Range{}, false},
		{"edit covering the range touches", Edit{Pos: 5, Removed: 20, Added: 0}, Range{}, false},
		{"insert exactly at the end touches", Edit{Pos: 15, Removed: 0, Added: 1}, Range{}, false},
		{"delete starting at the end touches", Edit{Pos: 15, Removed: 2, Added: 0}, Range{}, false},
		{"insert one unit after the end is far", Edit{Pos: 16, Removed: 0, Added: 4}, r, true},
		{"edit far after is ignored", Edit{Pos: 100, Removed: 5, Added: 0}, r, true},
		{"deleting a lot far before moves it to the start", Edit{Pos: 0, Removed: 9, Added: 0}, Range{1, 6}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := r.Rebase(tt.edit)
			if ok != tt.wantOK || (ok && got != tt.want) {
				t.Errorf("Rebase(%+v) = %+v, %v; want %+v, %v", tt.edit, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
