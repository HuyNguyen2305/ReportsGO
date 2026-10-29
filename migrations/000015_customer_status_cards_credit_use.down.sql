DROP TABLE credit_applications;
DROP TABLE customer_payment_methods;

DROP INDEX customers_status_idx;
ALTER TABLE customers DROP COLUMN status;
DROP TYPE customer_status;
