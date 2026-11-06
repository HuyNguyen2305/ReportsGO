-- Online Bookings: whether a customer came from an online booking. The report lists the jobs of these customers, with the
-- customer's own lead source shown beside them. Nothing derives the flag from the source: it is set per customer.

ALTER TABLE customers ADD COLUMN booked_online boolean NOT NULL DEFAULT false;

-- The report only ever looks at flagged customers, which are few.
CREATE INDEX customers_booked_online_idx ON customers (id) WHERE booked_online;
