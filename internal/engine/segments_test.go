package engine

import "testing"

func TestPlanSegments(t *testing.T) {
	segs := PlanSegments(1<<20, 4)
	if len(segs) != 2 {
		t.Fatalf("segments = %d, want 2", len(segs))
	}
	if segs[0].Start != 0 || segs[0].End != 524287 {
		t.Errorf("seg0 = %+v", segs[0])
	}
	if segs[1].Start != 524288 || segs[1].End != 1048575 {
		t.Errorf("seg1 = %+v", segs[1])
	}
	var offset int64
	for _, s := range segs {
		if s.Start != offset {
			t.Fatalf("gap at %d", offset)
		}
		offset = s.End + 1
	}
	if offset != 1<<20 {
		t.Fatalf("coverage = %d", offset)
	}

	small := PlanSegments(1000, 16)
	if len(small) != 1 || small[0].Start != 0 || small[0].End != 999 {
		t.Fatalf("small = %+v", small)
	}
	if PlanSegments(0, 16) != nil {
		t.Fatal("want nil for empty size")
	}
}
