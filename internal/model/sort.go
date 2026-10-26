package model

// MaxSortTextLength is how many characters of a text sort value (customer name, job label, email, ...) take
// part in ordering. The report queries compare and order by that prefix only, with the row id breaking ties, so
// a sort value, and therefore a cursor, stays small however long the stored text is. Rows that share their
// first MaxSortTextLength characters are ordered by id.
const MaxSortTextLength = 200
