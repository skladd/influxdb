package inmem

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/influxdata/influxdb/models"
	"github.com/influxdata/influxdb/query"
	"github.com/influxdata/influxdb/tsdb"
	"github.com/influxdata/influxql"
)

// Test comparing SeriesIDs for equality.
func TestSeriesIDs_Equals(t *testing.T) {
	ids1 := seriesIDs([]uint64{1, 2, 3})
	ids2 := seriesIDs([]uint64{1, 2, 3})
	ids3 := seriesIDs([]uint64{4, 5, 6})

	if !ids1.Equals(ids2) {
		t.Fatal("expected ids1 == ids2")
	} else if ids1.Equals(ids3) {
		t.Fatal("expected ids1 != ids3")
	}
}

// Test intersecting sets of SeriesIDs.
func TestSeriesIDs_Intersect(t *testing.T) {
	// Test swapping l & r, all branches of if-else, and exit loop when 'j < len(r)'
	ids1 := seriesIDs([]uint64{1, 3, 4, 5, 6})
	ids2 := seriesIDs([]uint64{1, 2, 3, 7})
	exp := seriesIDs([]uint64{1, 3})
	got := ids1.Intersect(ids2)

	if !exp.Equals(got) {
		t.Fatalf("exp=%v, got=%v", exp, got)
	}

	// Test exit for loop when 'i < len(l)'
	ids1 = seriesIDs([]uint64{1})
	ids2 = seriesIDs([]uint64{1, 2})
	exp = seriesIDs([]uint64{1})
	got = ids1.Intersect(ids2)

	if !exp.Equals(got) {
		t.Fatalf("exp=%v, got=%v", exp, got)
	}
}

// Test union sets of SeriesIDs.
func TestSeriesIDs_Union(t *testing.T) {
	// Test all branches of if-else, exit loop because of 'j < len(r)', and append remainder from left.
	ids1 := seriesIDs([]uint64{1, 2, 3, 7})
	ids2 := seriesIDs([]uint64{1, 3, 4, 5, 6})
	exp := seriesIDs([]uint64{1, 2, 3, 4, 5, 6, 7})
	got := ids1.Union(ids2)

	if !exp.Equals(got) {
		t.Fatalf("exp=%v, got=%v", exp, got)
	}

	// Test exit because of 'i < len(l)' and append remainder from right.
	ids1 = seriesIDs([]uint64{1})
	ids2 = seriesIDs([]uint64{1, 2})
	exp = seriesIDs([]uint64{1, 2})
	got = ids1.Union(ids2)

	if !exp.Equals(got) {
		t.Fatalf("exp=%v, got=%v", exp, got)
	}
}

// Test removing one set of SeriesIDs from another.
func TestSeriesIDs_Reject(t *testing.T) {
	// Test all branches of if-else, exit loop because of 'j < len(r)', and append remainder from left.
	ids1 := seriesIDs([]uint64{1, 2, 3, 7})
	ids2 := seriesIDs([]uint64{1, 3, 4, 5, 6})
	exp := seriesIDs([]uint64{2, 7})
	got := ids1.Reject(ids2)

	if !exp.Equals(got) {
		t.Fatalf("exp=%v, got=%v", exp, got)
	}

	// Test exit because of 'i < len(l)'.
	ids1 = seriesIDs([]uint64{1})
	ids2 = seriesIDs([]uint64{1, 2})
	exp = seriesIDs{}
	got = ids1.Reject(ids2)

	if !exp.Equals(got) {
		t.Fatalf("exp=%v, got=%v", exp, got)
	}
}

func TestMeasurement_AddSeries_Nil(t *testing.T) {
	m := newMeasurement("foo", "cpu")
	if m.AddSeries(nil) {
		t.Fatalf("AddSeries mismatch: exp false, got true")
	}
}

func TestMeasurement_AppendSeriesKeysByID_Missing(t *testing.T) {
	m := newMeasurement("foo", "cpu")
	var dst []string
	dst = m.AppendSeriesKeysByID(dst, []uint64{1})
	if exp, got := 0, len(dst); exp != got {
		t.Fatalf("series len mismatch: exp %v, got %v", exp, got)
	}
}

func TestMeasurement_AppendSeriesKeysByID_Exists(t *testing.T) {
	m := newMeasurement("foo", "cpu")
	s := newSeries(1, m, "cpu,host=foo", models.Tags{models.NewTag([]byte("host"), []byte("foo"))})
	m.AddSeries(s)

	var dst []string
	dst = m.AppendSeriesKeysByID(dst, []uint64{1})
	if exp, got := 1, len(dst); exp != got {
		t.Fatalf("series len mismatch: exp %v, got %v", exp, got)
	}

	if exp, got := "cpu,host=foo", dst[0]; exp != got {
		t.Fatalf("series mismatch: exp %v, got %v", exp, got)
	}
}

func TestMeasurement_TagsSet_Deadlock(t *testing.T) {
	m := newMeasurement("foo", "cpu")
	s1 := newSeries(1, m, "cpu,host=foo", models.Tags{models.NewTag([]byte("host"), []byte("foo"))})
	m.AddSeries(s1)

	s2 := newSeries(2, m, "cpu,host=bar", models.Tags{models.NewTag([]byte("host"), []byte("bar"))})
	m.AddSeries(s2)

	m.DropSeries(s1)

	// This was deadlocking
	s := tsdb.NewSeriesIDSet()
	s.Add(1)
	m.TagSets(s, query.IteratorOptions{})
	if got, exp := len(m.SeriesIDs()), 1; got != exp {
		t.Fatalf("series count mismatch: got %v, exp %v", got, exp)
	}
}

// Ensures the tagKeyValue API contains no deadlocks or sync issues.
func TestTagKeyValue_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	done := make(chan struct{})
	time.AfterFunc(2*time.Second, func() { close(done) })

	v := newTagKeyValue()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			rand := rand.New(rand.NewSource(int64(i)))
			for {
				// Continue running until time limit.
				select {
				case <-done:
					return
				default:
				}

				// Randomly choose next API.
				switch rand.Intn(7) {
				case 0:
					v.bytes()
				case 1:
					v.Cardinality()
				case 2:
					v.Contains(string(byte(rand.Intn(52) + 65)))
				case 3:
					v.InsertSeriesIDByte([]byte{byte(rand.Intn(52)+65)}, rand.Uint64()%1000)
				case 4:
					v.Load(string(byte(rand.Intn(52) + 65)))
				case 5:
					v.Range(func(tagValue string, a seriesIDs) bool {
						return rand.Intn(10) == 0
					})
				case 6:
					v.RangeAll(func(k string, a seriesIDs) {})
				}
			}
		}(i)
	}
	wg.Wait()
}

func BenchmarkMeasurement_SeriesIDForExp_EQRegex(b *testing.B) {
	m := newMeasurement("foo", "cpu")
	for i := 0; i < 100000; i++ {
		s := newSeries(uint64(i), m, "cpu", models.Tags{models.NewTag(
			[]byte("host"),
			[]byte(fmt.Sprintf("host%d", i)))})
		m.AddSeries(s)
	}

	if exp, got := 100000, len(m.SeriesKeys()); exp != got {
		b.Fatalf("series count mismatch: exp %v got %v", exp, got)
	}

	stmt, err := influxql.NewParser(strings.NewReader(`SELECT * FROM cpu WHERE host =~ /host\d+/`)).ParseStatement()
	if err != nil {
		b.Fatalf("invalid statement: %s", err)
	}

	selectStmt := stmt.(*influxql.SelectStatement)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ids := m.IDsForExpr(selectStmt.Condition.(*influxql.BinaryExpr))
		if exp, got := 100000, len(ids); exp != got {
			b.Fatalf("series count mismatch: exp %v got %v", exp, got)
		}

	}
}

func BenchmarkMeasurement_SeriesIDForExp_NERegex(b *testing.B) {
	m := newMeasurement("foo", "cpu")
	for i := 0; i < 100000; i++ {
		s := newSeries(uint64(i), m, "cpu", models.Tags{models.Tag{
			Key:   []byte("host"),
			Value: []byte(fmt.Sprintf("host%d", i))}})
		m.AddSeries(s)
	}

	if exp, got := 100000, len(m.SeriesKeys()); exp != got {
		b.Fatalf("series count mismatch: exp %v got %v", exp, got)
	}

	stmt, err := influxql.NewParser(strings.NewReader(`SELECT * FROM cpu WHERE host !~ /foo\d+/`)).ParseStatement()
	if err != nil {
		b.Fatalf("invalid statement: %s", err)
	}

	selectStmt := stmt.(*influxql.SelectStatement)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ids := m.IDsForExpr(selectStmt.Condition.(*influxql.BinaryExpr))
		if exp, got := 100000, len(ids); exp != got {
			b.Fatalf("series count mismatch: exp %v got %v", exp, got)
		}

	}

}

func benchmarkTagSets(b *testing.B, n int, opt query.IteratorOptions) {
	m := newMeasurement("foo", "m")
	ss := tsdb.NewSeriesIDSet()

	for i := 0; i < n; i++ {
		tags := map[string]string{"tag1": "value1", "tag2": "value2"}
		s := newSeries(uint64(i), m, fmt.Sprintf("m,tag1=value1,tag2=value2"), models.NewTags(tags))
		ss.Add(uint64(i))
		m.AddSeries(s)
	}

	// warm caches
	m.TagSets(ss, opt)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.TagSets(ss, opt)
	}
}

func BenchmarkMeasurement_TagSetsNoDimensions_1000(b *testing.B) {
	benchmarkTagSets(b, 1000, query.IteratorOptions{})
}

func BenchmarkMeasurement_TagSetsDimensions_1000(b *testing.B) {
	benchmarkTagSets(b, 1000, query.IteratorOptions{Dimensions: []string{"tag1", "tag2"}})
}

func BenchmarkMeasurement_TagSetsNoDimensions_100000(b *testing.B) {
	benchmarkTagSets(b, 100000, query.IteratorOptions{})
}

func BenchmarkMeasurement_TagSetsDimensions_100000(b *testing.B) {
	benchmarkTagSets(b, 100000, query.IteratorOptions{Dimensions: []string{"tag1", "tag2"}})
}
