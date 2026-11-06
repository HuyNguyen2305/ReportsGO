package service_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"reportsgo/internal/model"
	"reportsgo/internal/service"
	servicemocks "reportsgo/tests/mocks/service"
)

func inboundLeadRows(n int) []model.InboundLeadRow {
	out := make([]model.InboundLeadRow, n)
	for i := range out {
		out[i] = model.InboundLeadRow{
			ID: int64(i + 1), CustomerName: fmt.Sprint("Lead ", i),
			SortValue: fmt.Sprintf("2026-10-%02dT08:00:00.000000Z", i+1),
		}
	}
	return out
}

// runInboundLeads calls List on a service whose repository returns rows, and returns the query it received.
func runInboundLeads(t *testing.T, p service.ListInboundLeadsParams, rows []model.InboundLeadRow) (model.InboundLeadQuery, *service.ListInboundLeadsResult) {
	t.Helper()
	repo := servicemocks.NewMockInboundLeadRepository(t)
	var got model.InboundLeadQuery
	repo.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, q model.InboundLeadQuery) { got = q }).
		Return(rows, nil).Once()

	res, err := service.NewInboundLeadService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

// rejectsInboundLeads asserts that p is invalid and that the repository is never touched.
func rejectsInboundLeads(t *testing.T, p service.ListInboundLeadsParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockInboundLeadRepository(t) // no expectations: any call fails the test
	_, err := service.NewInboundLeadService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

func TestInboundLeadListDefaults(t *testing.T) {
	q, res := runInboundLeads(t, service.ListInboundLeadsParams{}, nil)

	require.Equal(t, model.InboundLeadQuery{
		Filter: model.InboundLeadFilter{
			CreatedFrom:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			CreatedBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		},
		NameFormat: model.NameFirst,
		Sort:       model.InboundLeadSort{Key: model.InboundLeadSortDateAdded, Desc: true},
		Limit:      service.DefaultPageSize,
	}, q, "the current month, every source, first names, newest first")
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
}

func TestInboundLeadListParsesFilters(t *testing.T) {
	q, _ := runInboundLeads(t, service.ListInboundLeadsParams{
		From: "2026-11-01", To: "2026-11-30", SourceIDs: []string{"2", " NONE ", "2", "5"},
		NameFormat: " Full_Name ", Search: "  mia ", Sort: "source", Limit: "10",
	}, nil)

	require.Equal(t, date(2026, 11, 1), q.Filter.CreatedFrom)
	require.Equal(t, date(2026, 12, 1), q.Filter.CreatedBefore, "the last day is included, so the bound is the next day")
	require.Equal(t, []int64{2, 5}, q.Filter.LeadSourceIDs)
	require.True(t, q.Filter.NoSource, `"none" is the leads without a source`)
	require.Equal(t, "mia", q.Filter.Search)
	require.Equal(t, model.NameFull, q.NameFormat)
	require.Equal(t, model.InboundLeadSort{Key: model.InboundLeadSortSource}, q.Sort, "a sort key alone is ascending")
	require.Equal(t, 10, q.Limit)
}

func TestInboundLeadListSourceFilterForms(t *testing.T) {
	tests := []struct {
		name     string
		raw      []string
		wantIDs  []int64
		wantNone bool
	}{
		{"nothing", nil, nil, false},
		{"ids only", []string{"3", "4"}, []int64{3, 4}, false},
		{"none only", []string{"none"}, nil, true},
		{"none is case-insensitive", []string{"None", "NONE"}, nil, true},
		{"ids and none", []string{"1", "none"}, []int64{1}, true},
		{"blanks are ignored", []string{"", " "}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runInboundLeads(t, service.ListInboundLeadsParams{SourceIDs: tt.raw}, nil)

			require.Equal(t, tt.wantIDs, q.Filter.LeadSourceIDs)
			require.Equal(t, tt.wantNone, q.Filter.NoSource)
		})
	}
}

func TestInboundLeadListEveryNameFormatIsAccepted(t *testing.T) {
	for _, f := range model.NameFormats {
		q, _ := runInboundLeads(t, service.ListInboundLeadsParams{NameFormat: string(f)}, nil)
		require.Equal(t, f, q.NameFormat)
	}
}

func TestInboundLeadListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.InboundLeadSort
	}{
		{"neither: newest first", "", "", model.InboundLeadSort{Key: model.InboundLeadSortDateAdded, Desc: true}},
		{"order alone flips the default key", "", "asc", model.InboundLeadSort{Key: model.InboundLeadSortDateAdded}},
		{"key alone is ascending", "customer", "", model.InboundLeadSort{Key: model.InboundLeadSortCustomer}},
		{"key and order", "notes", "DESC", model.InboundLeadSort{Key: model.InboundLeadSortNotes, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runInboundLeads(t, service.ListInboundLeadsParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}
}

func TestInboundLeadListValidation(t *testing.T) {
	tests := []struct {
		name      string
		p         service.ListInboundLeadsParams
		wantField string
	}{
		{"bad from", service.ListInboundLeadsParams{From: "11/01/2026"}, "from"},
		{"to before from", service.ListInboundLeadsParams{From: "2026-11-05", To: "2026-11-01"}, "to"},
		{"source id not a number", service.ListInboundLeadsParams{SourceIDs: []string{"abc"}}, "source_id"},
		{"source id zero", service.ListInboundLeadsParams{SourceIDs: []string{"0"}}, "source_id"},
		{"source id negative", service.ListInboundLeadsParams{SourceIDs: []string{"-1"}}, "source_id"},
		{"too many source ids", service.ListInboundLeadsParams{SourceIDs: make([]string, service.MaxIDsPerFilter+1)}, "source_id"},
		{"unknown name format", service.ListInboundLeadsParams{NameFormat: "nickname"}, "name_format"},
		{"search too long", service.ListInboundLeadsParams{Search: strings.Repeat("x", service.MaxSearchLength+1)}, "q"},
		{"unknown sort", service.ListInboundLeadsParams{Sort: "active_job"}, "sort"},
		{"time added is not a separate column", service.ListInboundLeadsParams{Sort: "time_added"}, "sort"},
		{"unknown order", service.ListInboundLeadsParams{Order: "up"}, "order"},
		{"limit zero", service.ListInboundLeadsParams{Limit: "0"}, "limit"},
		{"limit too big", service.ListInboundLeadsParams{Limit: "201"}, "limit"},
		{"garbage cursor", service.ListInboundLeadsParams{Cursor: "!!!"}, "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Contains(t, rejectsInboundLeads(t, tt.p), tt.wantField)
		})
	}

	t.Run("every problem is reported together", func(t *testing.T) {
		got := rejectsInboundLeads(t, service.ListInboundLeadsParams{SourceIDs: []string{"x"}, NameFormat: "y", Limit: "0"})
		require.Contains(t, got, "source_id")
		require.Contains(t, got, "name_format")
		require.Contains(t, got, "limit")
	})

	t.Run("a cursor is not checked when the sort is invalid", func(t *testing.T) {
		got := rejectsInboundLeads(t, service.ListInboundLeadsParams{Sort: "nope", Cursor: "!!!"})
		require.NotContains(t, got, "cursor")
	})
}

func TestInboundLeadListPaging(t *testing.T) {
	tests := []struct {
		name        string
		limit       string
		fromRepo    int
		wantItems   int
		wantHasMore bool
	}{
		{"short page", "5", 3, 3, false},
		{"exactly a full page", "5", 5, 5, false},
		{"one row beyond: more pages", "5", 6, 5, true},
		{"empty", "5", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, res := runInboundLeads(t, service.ListInboundLeadsParams{Limit: tt.limit}, inboundLeadRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.NotNil(t, res.Items, "an empty page is an empty list, never nil")
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}
			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodeInboundLeadCursor(res.NextCursor, model.InboundLeadSort{Key: model.InboundLeadSortDateAdded, Desc: true})
			require.NoError(t, err)
			require.Equal(t, &model.InboundLeadCursor{SortValue: last.SortValue, ID: last.ID}, cur)
		})
	}
}

func TestInboundLeadListAppliesTheCursor(t *testing.T) {
	sort := model.InboundLeadSort{Key: model.InboundLeadSortDateAdded, Desc: true}
	cursor := service.EncodeInboundLeadCursor(sort, model.InboundLeadRow{ID: 9, SortValue: "2026-10-15T14:30:15.123456Z"})

	q, _ := runInboundLeads(t, service.ListInboundLeadsParams{Cursor: cursor}, nil)

	require.Equal(t, &model.InboundLeadCursor{SortValue: "2026-10-15T14:30:15.123456Z", ID: 9}, q.After)
}

func TestInboundLeadListRejectsACursorOfAnotherSort(t *testing.T) {
	cursor := service.EncodeInboundLeadCursor(model.InboundLeadSort{Key: model.InboundLeadSortEmail},
		model.InboundLeadRow{ID: 9, SortValue: "a@example.com"})

	got := rejectsInboundLeads(t, service.ListInboundLeadsParams{Cursor: cursor})

	require.Contains(t, got["cursor"], "does not match the requested sort")
}

func TestInboundLeadListWrapsRepositoryErrors(t *testing.T) {
	repo := servicemocks.NewMockInboundLeadRepository(t)
	repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

	res, err := service.NewInboundLeadService(repo, fixedClock(october21)).List(context.Background(), service.ListInboundLeadsParams{})

	require.Nil(t, res)
	require.ErrorIs(t, err, errBoom)
	require.ErrorContains(t, err, "list inbound leads")
	require.NotErrorIs(t, err, model.ErrInvalidInput)
}

func TestInboundLeadFilterOptions(t *testing.T) {
	t.Run("a nil list becomes an empty list", func(t *testing.T) {
		repo := servicemocks.NewMockInboundLeadRepository(t)
		repo.EXPECT().LeadSources(mock.Anything).Return(nil, nil).Once()

		opts, err := service.NewInboundLeadService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.NotNil(t, opts.Sources)
	})

	t.Run("a failing lookup is wrapped", func(t *testing.T) {
		repo := servicemocks.NewMockInboundLeadRepository(t)
		repo.EXPECT().LeadSources(mock.Anything).Return(nil, errBoom).Once()

		opts, err := service.NewInboundLeadService(repo, nil).FilterOptions(context.Background())

		require.Nil(t, opts)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "load filter options")
	})
}

// inboundLeadSortValue is what the repository's sort_value column looks like for each key.
var inboundLeadSortValue = map[model.InboundLeadSortKey]string{
	model.InboundLeadSortCustomer:  "william saliba",
	model.InboundLeadSortSource:    "google",
	model.InboundLeadSortEmail:     "mia.torres@example.com",
	model.InboundLeadSortPhone:     "(555) 010-0001",
	model.InboundLeadSortNotes:     "asked for a quote",
	model.InboundLeadSortDateAdded: "2026-11-02T08:15:30.123456Z",
}

func TestInboundLeadCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, inboundLeadSortValue, len(model.InboundLeadSortKeys), "every sort key needs a sample value")

	for _, key := range model.InboundLeadSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.InboundLeadSort{Key: key, Desc: desc}
				row := model.InboundLeadRow{ID: 42, SortValue: inboundLeadSortValue[key]}

				got, err := service.DecodeInboundLeadCursor(service.EncodeInboundLeadCursor(sort, row), sort)

				require.NoError(t, err)
				require.Equal(t, &model.InboundLeadCursor{SortValue: inboundLeadSortValue[key], ID: 42}, got)
			})
		}
	}
}

func TestInboundLeadCursorAcceptsEmptyValues(t *testing.T) {
	for _, key := range []model.InboundLeadSortKey{
		model.InboundLeadSortSource, model.InboundLeadSortEmail, model.InboundLeadSortPhone, model.InboundLeadSortNotes,
	} {
		sort := model.InboundLeadSort{Key: key}
		raw := service.EncodeInboundLeadCursor(sort, model.InboundLeadRow{ID: 5, SortValue: ""})

		got, err := service.DecodeInboundLeadCursor(raw, sort)

		require.NoError(t, err, key)
		require.Equal(t, "", got.SortValue, "%s: a lead without one sorts as an empty value", key)
	}
}

func TestInboundLeadCursorRejectsInvalidCursors(t *testing.T) {
	dateAsc := model.InboundLeadSort{Key: model.InboundLeadSortDateAdded}
	emailAsc := model.InboundLeadSort{Key: model.InboundLeadSortEmail}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name       string
		raw        string
		sort       model.InboundLeadSort
		wantReason string
	}{
		{"empty", "", dateAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", dateAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"date_added","d":false,"v":"2026-10-12T08:00:00.000000Z","id":0}`), dateAsc, "not a valid cursor"},
		{"other sort key", cursorWith("notes", "x"), dateAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"date_added","d":true,"v":"2026-10-12T08:00:00.000000Z","id":1}`), dateAsc, "does not match the requested sort"},
		{"timestamp without microseconds", cursorWith("date_added", "2026-10-12T08:00:00Z"), dateAsc, "not a valid cursor"},
		{"timestamp with an offset", cursorWith("date_added", "2026-10-12T08:00:00.000000+07:00"), dateAsc, "not a valid cursor"},
		{"timestamp with injection", cursorWith("date_added", "2026-10-12T08:00:00.000000Z'; DROP TABLE customers;--"), dateAsc, "not a valid cursor"},
		{"text with NUL", encode(`{"k":"email","d":false,"v":"a\u0000b","id":1}`), emailAsc, "not a valid cursor"},
		{"text too long", cursorWith("email", strings.Repeat("x", model.MaxSortTextLength+1)), emailAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), emailAsc, "too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeInboundLeadCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}
