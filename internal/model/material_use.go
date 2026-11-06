package model

import "time"

// MaterialUseTab is the All / Jobs / Devices switch of the Material Use report.
type MaterialUseTab string

// Material Use tabs. Jobs keeps the materials added without a device, Devices those added with one.
const (
	MaterialUseAll     MaterialUseTab = "all"
	MaterialUseJobs    MaterialUseTab = "jobs"
	MaterialUseDevices MaterialUseTab = "devices"
)

// MaterialUseTabs lists every valid MaterialUseTab.
var MaterialUseTabs = []MaterialUseTab{MaterialUseAll, MaterialUseJobs, MaterialUseDevices}

// MaterialUseSortKey names a column the Material Use report can be sorted by. It is a closed set: the repository
// maps each key to a fixed SQL expression, so no caller-supplied text ever reaches the SQL.
type MaterialUseSortKey string

// Material Use sort keys.
const (
	MaterialUseSortMaterial     MaterialUseSortKey = "material"
	MaterialUseSortEPA          MaterialUseSortKey = "epa"
	MaterialUseSortQty          MaterialUseSortKey = "qty"
	MaterialUseSortApplications MaterialUseSortKey = "applications"
)

// MaterialUseSortKeys lists every valid MaterialUseSortKey.
var MaterialUseSortKeys = []MaterialUseSortKey{
	MaterialUseSortMaterial, MaterialUseSortEPA, MaterialUseSortQty, MaterialUseSortApplications,
}

// MaterialUseSort is a sort key and direction. Ties are always broken by material id in the same direction.
type MaterialUseSort struct {
	Key  MaterialUseSortKey
	Desc bool
}

// MaterialUseFilter narrows the Material Use report. An empty list means "All". Materials added to a deleted job
// are always left out.
type MaterialUseFilter struct {
	// ScheduledFrom is the first instant included and ScheduledBefore the first instant excluded, on the date the
	// job occurs (jobs.scheduled_at). The service builds them from calendar days in the business time zone. A zero
	// value leaves that side open; a job that was never scheduled is left out as soon as either side is set.
	ScheduledFrom, ScheduledBefore time.Time
	Tab                            MaterialUseTab
	CustomerIDs                    []int64
	MaterialIDs                    []int64
	// Search matches, case-insensitively and as a substring, the material name and its EPA number. It is plain
	// text: LIKE wildcards in it are matched literally.
	Search string
}

// MaterialUseCursor identifies the last row of a page: its sort value (as text) and material id.
type MaterialUseCursor struct {
	SortValue string
	ID        int64
}

// MaterialUseQuery is one page request. Limit is the number of rows wanted; the repository returns up to Limit+1
// so the caller can tell whether another page exists.
type MaterialUseQuery struct {
	Filter MaterialUseFilter
	Sort   MaterialUseSort
	Limit  int
	After  *MaterialUseCursor
}

// MaterialUseRow is one line of the report: one material, added up over the jobs in scope.
type MaterialUseRow struct {
	MaterialID int64
	Material   string
	// EPA is the material's EPA registration number, nil when it has none.
	EPA *string
	// Qty is the total quantity as a decimal with three places, for example "12.500".
	Qty string
	// Applications is the number of times the material was added to a job.
	Applications int64
	// SortValue is this row's value for the sort key, as text, used to build the next page's cursor.
	SortValue string
}

// MaterialOption is a material offered by the report's Materials filter.
type MaterialOption struct {
	ID   int64
	Name string
}

// ClientOption is a customer offered by the report's Clients filter.
type ClientOption struct {
	ID   int64
	Name string
}
