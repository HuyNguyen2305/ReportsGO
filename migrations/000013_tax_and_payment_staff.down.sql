DROP INDEX payments_received_by_idx;
ALTER TABLE payments DROP COLUMN received_by;

DROP TABLE invoice_tax_lines;
DROP TABLE tax_rates;
