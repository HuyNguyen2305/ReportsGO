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

func materialUseRows(n int) []model.MaterialUseRow {
	out := make([]model.MaterialUseRow, n)
	for i := range out {
		out[i] = model.MaterialUseRow{
			MaterialID: int64(i + 1), Material: fmt.Sprint("Material ", i), Qty: "1.000", Applications: 1,
			SortValue: fmt.Sprintf("material %02d", i),
		}
	}
	return out
}

// runMaterialUse calls List on a service whose repository returns rows, and returns the query it received.
func runMaterialUse(t *testing.T, p service.ListMaterialUseParams, rows []model.MaterialUseRow) (model.MaterialUseQuery, *service.ListMaterialUseResult) {
	t.Helper()
	repo := servicemocks.NewMockMaterialUseRepository(t)
	var got model.MaterialUseQuery
	repo.EXPECT().List(mock.Anything, mock.Anything).
		Run(func(_ context.Context, q model.MaterialUseQuery) { got = q }).
		Return(rows, nil).Once()

	res, err := service.NewMaterialUseService(repo, fixedClock(october21)).List(context.Background(), p)
	require.NoError(t, err)
	return got, res
}

// rejectsMaterialUse asserts that p is invalid and that the repository is never touched.
func rejectsMaterialUse(t *testing.T, p service.ListMaterialUseParams) map[string]string {
	t.Helper()
	repo := servicemocks.NewMockMaterialUseRepository(t) // no expectations: any call fails the test
	_, err := service.NewMaterialUseService(repo, fixedClock(october21)).List(context.Background(), p)
	require.Error(t, err)
	return fields(t, err)
}

func TestMaterialUseListDefaults(t *testing.T) {
	q, res := runMaterialUse(t, service.ListMaterialUseParams{}, nil)

	require.Equal(t, model.MaterialUseQuery{
		Filter: model.MaterialUseFilter{
			ScheduledFrom:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			ScheduledBefore: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
			Tab:             model.MaterialUseAll,
		},
		Sort:  model.MaterialUseSort{Key: model.MaterialUseSortMaterial, Desc: false},
		Limit: service.DefaultPageSize,
	}, q, "the current month, every tab, material A to Z")
	require.Equal(t, date(2026, 10, 1), res.From)
	require.Equal(t, date(2026, 10, 31), res.To)
}

func TestMaterialUseListParsesFilters(t *testing.T) {
	q, _ := runMaterialUse(t, service.ListMaterialUseParams{
		From: "2026-11-01", To: "2026-11-30", Tab: " Devices ", CustomerIDs: []string{"3", "3", "5"},
		MaterialIDs: []string{"2"}, Search: "  demand ", Sort: "qty", Limit: "10",
	}, nil)

	require.Equal(t, date(2026, 11, 1), q.Filter.ScheduledFrom)
	require.Equal(t, date(2026, 12, 1), q.Filter.ScheduledBefore, "the last day is included, so the bound is the next day")
	require.Equal(t, model.MaterialUseDevices, q.Filter.Tab)
	require.Equal(t, []int64{3, 5}, q.Filter.CustomerIDs)
	require.Equal(t, []int64{2}, q.Filter.MaterialIDs)
	require.Equal(t, "demand", q.Filter.Search)
	require.Equal(t, model.MaterialUseSort{Key: model.MaterialUseSortQty, Desc: false}, q.Sort, "a sort key alone is ascending")
	require.Equal(t, 10, q.Limit)
}

func TestMaterialUseListSortDefaults(t *testing.T) {
	tests := []struct {
		name        string
		sort, order string
		want        model.MaterialUseSort
	}{
		{"neither: name A to Z", "", "", model.MaterialUseSort{Key: model.MaterialUseSortMaterial}},
		{"order alone flips the default key", "", "desc", model.MaterialUseSort{Key: model.MaterialUseSortMaterial, Desc: true}},
		{"key alone is ascending", "applications", "", model.MaterialUseSort{Key: model.MaterialUseSortApplications}},
		{"key and order", "epa", "DESC", model.MaterialUseSort{Key: model.MaterialUseSortEPA, Desc: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := runMaterialUse(t, service.ListMaterialUseParams{Sort: tt.sort, Order: tt.order}, nil)
			require.Equal(t, tt.want, q.Sort)
		})
	}
}

func TestMaterialUseListValidation(t *testing.T) {
	tests := []struct {
		name      string
		p         service.ListMaterialUseParams
		wantField string
	}{
		{"bad from", service.ListMaterialUseParams{From: "11/01/2026"}, "from"},
		{"to before from", service.ListMaterialUseParams{From: "2026-11-05", To: "2026-11-01"}, "to"},
		{"unknown tab", service.ListMaterialUseParams{Tab: "devicez"}, "tab"},
		{"client id not a number", service.ListMaterialUseParams{CustomerIDs: []string{"abc"}}, "client_id"},
		{"client id zero", service.ListMaterialUseParams{CustomerIDs: []string{"0"}}, "client_id"},
		{"material id negative", service.ListMaterialUseParams{MaterialIDs: []string{"-1"}}, "material_id"},
		{"too many material ids", service.ListMaterialUseParams{MaterialIDs: make([]string, service.MaxIDsPerFilter+1)}, "material_id"},
		{"search too long", service.ListMaterialUseParams{Search: strings.Repeat("x", service.MaxSearchLength+1)}, "q"},
		{"unknown sort", service.ListMaterialUseParams{Sort: "unit"}, "sort"},
		{"unknown order", service.ListMaterialUseParams{Order: "up"}, "order"},
		{"limit zero", service.ListMaterialUseParams{Limit: "0"}, "limit"},
		{"limit too big", service.ListMaterialUseParams{Limit: "201"}, "limit"},
		{"garbage cursor", service.ListMaterialUseParams{Cursor: "!!!"}, "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Contains(t, rejectsMaterialUse(t, tt.p), tt.wantField)
		})
	}

	t.Run("every problem is reported together", func(t *testing.T) {
		got := rejectsMaterialUse(t, service.ListMaterialUseParams{Tab: "x", Limit: "0", Sort: "nope"})
		require.Contains(t, got, "tab")
		require.Contains(t, got, "limit")
		require.Contains(t, got, "sort")
	})

	t.Run("a cursor is not checked when the sort is invalid", func(t *testing.T) {
		got := rejectsMaterialUse(t, service.ListMaterialUseParams{Sort: "nope", Cursor: "!!!"})
		require.NotContains(t, got, "cursor")
	})
}

func TestMaterialUseListPaging(t *testing.T) {
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
			_, res := runMaterialUse(t, service.ListMaterialUseParams{Limit: tt.limit}, materialUseRows(tt.fromRepo))

			require.Len(t, res.Items, tt.wantItems)
			require.NotNil(t, res.Items, "an empty page is an empty list, never nil")
			require.Equal(t, tt.wantHasMore, res.HasMore)
			if !tt.wantHasMore {
				require.Empty(t, res.NextCursor)
				return
			}
			// the cursor resumes after the last row that was kept, not the extra one fetched
			last := res.Items[len(res.Items)-1]
			cur, err := service.DecodeMaterialUseCursor(res.NextCursor, model.MaterialUseSort{Key: model.MaterialUseSortMaterial})
			require.NoError(t, err)
			require.Equal(t, &model.MaterialUseCursor{SortValue: last.SortValue, ID: last.MaterialID}, cur)
		})
	}
}

func TestMaterialUseListAppliesTheCursor(t *testing.T) {
	sort := model.MaterialUseSort{Key: model.MaterialUseSortQty, Desc: true}
	cursor := service.EncodeMaterialUseCursor(sort, model.MaterialUseRow{MaterialID: 9, SortValue: "12.500"})

	q, _ := runMaterialUse(t, service.ListMaterialUseParams{Sort: "qty", Order: "desc", Cursor: cursor}, nil)

	require.Equal(t, &model.MaterialUseCursor{SortValue: "12.500", ID: 9}, q.After)
}

func TestMaterialUseListRejectsACursorOfAnotherSort(t *testing.T) {
	cursor := service.EncodeMaterialUseCursor(model.MaterialUseSort{Key: model.MaterialUseSortQty}, model.MaterialUseRow{MaterialID: 9, SortValue: "1.000"})

	got := rejectsMaterialUse(t, service.ListMaterialUseParams{Cursor: cursor})

	require.Contains(t, got["cursor"], "does not match the requested sort")
}

func TestMaterialUseListWrapsRepositoryErrors(t *testing.T) {
	repo := servicemocks.NewMockMaterialUseRepository(t)
	repo.EXPECT().List(mock.Anything, mock.Anything).Return(nil, errBoom).Once()

	res, err := service.NewMaterialUseService(repo, fixedClock(october21)).List(context.Background(), service.ListMaterialUseParams{})

	require.Nil(t, res)
	require.ErrorIs(t, err, errBoom)
	require.ErrorContains(t, err, "list material use")
	require.NotErrorIs(t, err, model.ErrInvalidInput)
}

func TestMaterialUseFilterOptions(t *testing.T) {
	t.Run("nil lists become empty lists", func(t *testing.T) {
		repo := servicemocks.NewMockMaterialUseRepository(t)
		repo.EXPECT().Materials(mock.Anything).Return(nil, nil).Once()
		repo.EXPECT().Clients(mock.Anything).Return(nil, nil).Once()

		opts, err := service.NewMaterialUseService(repo, nil).FilterOptions(context.Background())

		require.NoError(t, err)
		require.NotNil(t, opts.Materials)
		require.NotNil(t, opts.Clients)
	})

	t.Run("a failing lookup is wrapped", func(t *testing.T) {
		repo := servicemocks.NewMockMaterialUseRepository(t)
		repo.EXPECT().Materials(mock.Anything).Return(nil, errBoom).Once()

		opts, err := service.NewMaterialUseService(repo, nil).FilterOptions(context.Background())

		require.Nil(t, opts)
		require.ErrorIs(t, err, errBoom)
		require.ErrorContains(t, err, "load filter options")
	})

	t.Run("a failing client lookup is wrapped", func(t *testing.T) {
		repo := servicemocks.NewMockMaterialUseRepository(t)
		repo.EXPECT().Materials(mock.Anything).Return(nil, nil).Once()
		repo.EXPECT().Clients(mock.Anything).Return(nil, errBoom).Once()

		_, err := service.NewMaterialUseService(repo, nil).FilterOptions(context.Background())

		require.ErrorIs(t, err, errBoom)
	})
}

// materialUseSortValue is what the repository's sort_value column looks like for each key.
var materialUseSortValue = map[model.MaterialUseSortKey]string{
	model.MaterialUseSortMaterial:     "demand cs",
	model.MaterialUseSortEPA:          "100-1066",
	model.MaterialUseSortQty:          "12.500",
	model.MaterialUseSortApplications: "7",
}

func TestMaterialUseCursorRoundTripForEveryKeyAndDirection(t *testing.T) {
	require.Len(t, materialUseSortValue, len(model.MaterialUseSortKeys), "every sort key needs a sample value")

	for _, key := range model.MaterialUseSortKeys {
		for _, desc := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%v", key, desc), func(t *testing.T) {
				sort := model.MaterialUseSort{Key: key, Desc: desc}
				row := model.MaterialUseRow{MaterialID: 42, SortValue: materialUseSortValue[key]}

				got, err := service.DecodeMaterialUseCursor(service.EncodeMaterialUseCursor(sort, row), sort)

				require.NoError(t, err)
				require.Equal(t, &model.MaterialUseCursor{SortValue: materialUseSortValue[key], ID: 42}, got)
			})
		}
	}
}

func TestMaterialUseCursorAcceptsAnEmptyEPA(t *testing.T) {
	sort := model.MaterialUseSort{Key: model.MaterialUseSortEPA}
	raw := service.EncodeMaterialUseCursor(sort, model.MaterialUseRow{MaterialID: 5, SortValue: ""})

	got, err := service.DecodeMaterialUseCursor(raw, sort)

	require.NoError(t, err)
	require.Equal(t, "", got.SortValue, "a material without an EPA number sorts as an empty one")
}

func TestMaterialUseCursorRejectsInvalidCursors(t *testing.T) {
	qtyAsc := model.MaterialUseSort{Key: model.MaterialUseSortQty}
	appsAsc := model.MaterialUseSort{Key: model.MaterialUseSortApplications}
	materialAsc := model.MaterialUseSort{Key: model.MaterialUseSortMaterial}

	cursorWith := func(key, value string) string {
		return encode(`{"k":"` + key + `","d":false,"v":"` + value + `","id":1}`)
	}

	tests := []struct {
		name       string
		raw        string
		sort       model.MaterialUseSort
		wantReason string
	}{
		{"empty", "", qtyAsc, "not a valid cursor"},
		{"not base64", "!!!not-base64!!!", qtyAsc, "not a valid cursor"},
		{"id zero", encode(`{"k":"qty","d":false,"v":"1.000","id":0}`), qtyAsc, "not a valid cursor"},
		{"other sort key", cursorWith("material", "x"), qtyAsc, "does not match the requested sort"},
		{"other direction", encode(`{"k":"qty","d":true,"v":"1.000","id":1}`), qtyAsc, "does not match the requested sort"},
		{"qty not a number", cursorWith("qty", "abc"), qtyAsc, "not a valid cursor"},
		{"qty with injection", cursorWith("qty", "1; DROP TABLE jobs"), qtyAsc, "not a valid cursor"},
		{"qty empty", cursorWith("qty", ""), qtyAsc, "not a valid cursor"},
		{"qty with four decimals", cursorWith("qty", "1.2345"), qtyAsc, "not a valid cursor"},
		{"qty negative", cursorWith("qty", "-1.000"), qtyAsc, "not a valid cursor"},
		{"applications with a fraction letter", cursorWith("applications", "1.x"), appsAsc, "not a valid cursor"},
		{"text with NUL", encode(`{"k":"material","d":false,"v":"a\u0000b","id":1}`), materialAsc, "not a valid cursor"},
		{"text too long", cursorWith("material", strings.Repeat("x", model.MaxSortTextLength+1)), materialAsc, "not a valid cursor"},
		{"cursor too long", strings.Repeat("A", 4097), materialAsc, "too long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.DecodeMaterialUseCursor(tt.raw, tt.sort)

			require.Nil(t, got)
			var ce *service.InvalidCursorError
			require.ErrorAs(t, err, &ce)
			require.Contains(t, ce.Reason, tt.wantReason)
		})
	}
}

func TestMaterialUseCursorAcceptsBoundaryNumbers(t *testing.T) {
	sort := model.MaterialUseSort{Key: model.MaterialUseSortQty}
	for _, v := range []string{"0", "0.001", "7", "99999999.999", "123456789012345.999"} {
		raw := service.EncodeMaterialUseCursor(sort, model.MaterialUseRow{MaterialID: 1, SortValue: v})
		got, err := service.DecodeMaterialUseCursor(raw, sort)
		require.NoError(t, err, v)
		require.Equal(t, v, got.SortValue)
	}
}
