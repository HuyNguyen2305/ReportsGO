-- Development sample data mirroring the report screenshots (October 2026).
-- WARNING: truncates every data table first. Never run against production.
-- Rows reference each other by natural keys (account/invoice/estimate numbers),
-- so the script does not depend on identity values.

TRUNCATE payments, credits, documents, estimates, invoice_tag_links, recurring_invoices,
         invoices, jobs, service_locations, customer_phones, customers,
         document_types, invoice_tags, payment_terms, service_types, lead_sources, users
    RESTART IDENTITY CASCADE;

-- Reference data ---------------------------------------------------------

INSERT INTO users (full_name, email) VALUES
    ('Thọ Nguyễn Quang', 'tho.nguyen.quang@example.com'),
    ('thon nguyen',      'thon.nguyen@example.com');

INSERT INTO lead_sources (name) VALUES ('Facebook');

INSERT INTO service_types (name) VALUES
    ('Bi-Monthly Service'), ('Initial Service'), ('Blank Service 123'),
    ('Full service'), ('Call Back Service');

INSERT INTO payment_terms (name, description) VALUES
    ('Due on receipt', 'Payment is due on receipt.'),
    ('Net 30 Terms', 'Payment is due within 30 days from the invoice date. Invoices that are not settled within this period will incur a late payment fee of 5%.');

INSERT INTO invoice_tags (name) VALUES ('a'), ('b'), ('c');

INSERT INTO document_types (name) VALUES
    ('1-Time Treatment (PestPros of MI)'),
    ('1 Year SubTermite Agreement (LibertyPS)'),
    ('1 Year Termite Warranty (Spidermen)');

-- Customers, phones, locations -------------------------------------------

INSERT INTO customers (account_number, first_name, last_name, display_name, email, billing_email, lead_source_id, top_note, created_at)
SELECT v.acct, v.first_name, v.last_name, v.display_name, v.email, v.email, ls.id, v.top_note, v.created_at::timestamptz
FROM (VALUES
    ('5001',  'William', 'Saliba',  'William Saliba',  'williamsaliba@yopmail.com', NULL,                    '',                       '2026-09-01'),
    ('5002',  'Alaba',   'David',   'Alaba David',     'alaba123@yopmail.com',      NULL,                    '',                       '2026-09-01'),
    ('63689', 'Bukayo',  'Saka',    'Bukayo Saka',     'sakabukayo@yopmail.com',    NULL,                    '',                       '2026-09-14'),
    ('63701', '',        'PerfTest','PerfTest',        NULL,                        NULL,                    '',                       '2026-10-01'),
    ('63702', '',        'PerfTest2','PerfTest2',      NULL,                        NULL,                    '',                       '2026-10-04'),
    ('63706', 'AAA1',    'ccc1',    'AAA1 ccc1',       'AAAA123@yopmail.com',       NULL,                    '',                       '2026-10-05'),
    ('63714', '#F',      'Last Name','#F Last Name',   'd@cm.co',                   'Facebook',              'Here''s top note #F',    '2026-10-05'),
    ('63716', '#',       'D',       '# D',             NULL,                        NULL,                    '',                       '2026-10-05'),
    ('63717', '#',       'Â',       '# Â',             NULL,                        NULL,                    '',                       '2026-10-06'),
    ('63730', 'QA',      'customer','QA customer edited ad35e0', NULL,              NULL,                    '',                       '2026-10-08'),
    ('63733', 'Customer','1',       'Customer 1',      NULL,                        NULL,                    '',                       '2026-10-16')
) AS v(acct, first_name, last_name, display_name, email, source, top_note, created_at)
LEFT JOIN lead_sources ls ON ls.name = v.source;

INSERT INTO customer_phones (customer_id, number, kind)
SELECT c.id, v.number, 'mobile'
FROM (VALUES
    ('63714', '012222222'),
    ('63706', '(098) 765-4321'),
    ('63689', '233960066'),
    ('5002',  '234325235234')
) AS v(acct, number)
JOIN customers c ON c.account_number = v.acct;

INSERT INTO service_locations (customer_id, address_line1, address_line2, city, state, postal_code)
SELECT c.id, v.line1, v.line2, v.city, v.state, v.zip
FROM (VALUES
    ('5001',  'West Loop 1604 South', '',   'San Antonio', 'TX', '78264'),
    ('5002',  '12 Alaba Street',      '',   'Austin',      'TX', '78701'),
    ('63689', '2 3, 4, 5 6',          '',   'Austin',      'TX', '78702'),
    ('63701', '1 Perf S',             '',   'Austin',      'TX', '78701'),
    ('63702', '3 Perf St 3 Perf St',  '',   'Austin',      'TX', '78701'),
    ('63706', 'Miami Beach Boardwalk 2', '', 'Miami',      'FL', '33139'),
    ('63714', 'S1 S2',                '',   'City',        'State', 'zip'),
    ('63716', 'Shearwood Forest Drive', '', 'Bridgeport',  'WV', '26330'),
    ('63717', '8 I, K, K z',          '',   '',            '',   ''),
    ('63730', '1 Main St',            '',   'Austin',      'TX', '73301'),
    ('63733', 'Ewald Drive 12322 East Freeway', '', 'Penfield', 'NY', '14625')
) AS v(acct, line1, line2, city, state, zip)
JOIN customers c ON c.account_number = v.acct;

-- Jobs --------------------------------------------------------------------

INSERT INTO jobs (customer_id, location_id, service_type_id, lead_source_id, status, scheduled_at, invoice_value, created_at)
SELECT c.id, l.id, st.id, ls.id, v.status::job_status, v.created_at::timestamptz, v.value, v.created_at::timestamptz
FROM (VALUES
    ('63706', 'Blank Service 123',   'confirmed',   '2026-10-01 04:30', 0.00,  NULL),
    ('63706', 'Blank Service 123',   'unconfirmed', '2026-10-01 05:27', 0.00,  NULL),
    ('63701', 'Blank Service 123',   'confirmed',   '2026-10-01 11:07', 1.00,  NULL),
    ('5001',  'Bi-Monthly Service',  'completed',   '2026-10-02 01:00', 65.00, NULL),
    ('5001',  'Bi-Monthly Service',  'completed',   '2026-10-02 04:09', 65.00, NULL),
    ('5001',  'Bi-Monthly Service',  'completed',   '2026-10-04 21:30', 65.00, NULL),
    ('5001',  'Bi-Monthly Service',  'completed',   '2026-10-04 21:30', 65.00, NULL),
    ('63702', 'Blank Service 123',   'unconfirmed', '2026-10-04 11:53', 0.00,  NULL),
    ('63714', 'Blank Service 123',   'unconfirmed', '2026-10-05 02:38', 1.00,  'Facebook'),
    ('63706', 'Full service',        'completed',   '2026-10-05 06:21', 79.30, NULL),
    ('5001',  'Bi-Monthly Service',  'completed',   '2026-10-05 21:29', 65.00, NULL),
    ('5001',  'Bi-Monthly Service',  'completed',   '2026-10-06 10:19', 65.00, NULL),
    ('63706', 'Call Back Service',   'confirmed',   '2026-10-07 07:31', 10.33, NULL),
    ('63717', 'Bi-Monthly Service',  'confirmed',   '2026-10-07 09:25', 65.00, NULL),
    ('5001',  'Bi-Monthly Service',  'completed',   '2026-10-07 09:32', 65.00, NULL),
    ('63689', 'Bi-Monthly Service',  'confirmed',   '2026-10-07 23:34', 65.00, NULL),
    ('63689', 'Bi-Monthly Service',  'confirmed',   '2026-10-07 23:42', 65.00, NULL),
    ('63706', 'Bi-Monthly Service',  'completed',   '2026-10-08 04:30', 65.00, NULL),
    ('5001',  'Bi-Monthly Service',  'completed',   '2026-10-08 05:41', 65.00, NULL),
    ('63689', 'Initial Service',     'unconfirmed', '2026-10-08 14:25', 95.00, NULL),
    ('63689', 'Initial Service',     'unconfirmed', '2026-10-08 14:45', 95.00, NULL),
    ('63689', 'Initial Service',     'unconfirmed', '2026-10-09 09:00', 95.00, NULL)
) AS v(acct, service, status, created_at, value, source)
JOIN customers c          ON c.account_number = v.acct
JOIN service_locations l  ON l.customer_id = c.id
JOIN service_types st     ON st.name = v.service
LEFT JOIN lead_sources ls ON ls.name = v.source;

-- Seven more completed Bi-Monthly jobs for William Saliba (10/09 - 10/15).
INSERT INTO jobs (customer_id, location_id, service_type_id, status, scheduled_at, invoice_value, created_at)
SELECT c.id, l.id, st.id, 'completed', d, 65.00, d
FROM generate_series('2026-10-09 08:00'::timestamptz, '2026-10-15 08:00'::timestamptz, interval '1 day') AS d
CROSS JOIN customers c
JOIN service_locations l ON l.customer_id = c.id
CROSS JOIN service_types st
WHERE c.account_number = '5001' AND st.name = 'Bi-Monthly Service';

-- Invoices ----------------------------------------------------------------
-- job_id links to the customer's most recent job, when one exists.

INSERT INTO invoices (invoice_number, customer_id, job_id, billing_email, invoice_date, total, discount,
                      status, signature_status, payment_terms_id, frequency_label, created_by)
SELECT v.num, c.id,
       (SELECT j.id FROM jobs j WHERE j.customer_id = c.id ORDER BY j.created_at DESC, j.id DESC LIMIT 1),
       c.billing_email, v.inv_date::date, v.total, v.discount,
       v.status::invoice_status, 'draft', pt.id, v.freq, u.id
FROM (VALUES
    ('84936', '5001',  '2026-10-26', 65.00,  0.00, 'draft', 'Due on receipt', 'Weekly',        'thon nguyen'),
    ('85030', '63714', '2026-10-21', 65.00,  0.00, 'draft', 'Due on receipt', 'Daily',         'thon nguyen'),
    ('85011', '63706', '2026-10-15', 61.98,  0.00, 'paid',  'Due on receipt', 'With each job', 'thon nguyen'),
    ('85010', '63706', '2026-10-15', 19.63,  0.00, 'paid',  'Due on receipt', 'With each job', 'thon nguyen'),
    ('84942', '5001',  '2026-10-14', 60.00,  0.00, 'draft', 'Due on receipt', 'Daily',         'thon nguyen'),
    ('85028', '63714', '2026-10-13', 66.00,  0.00, 'draft', 'Due on receipt', 'Daily',         'thon nguyen'),
    ('85007', '5001',  '2026-10-13', 65.00,  0.00, 'void',  'Due on receipt', 'With each job', 'thon nguyen'),
    ('84937', '5001',  '2026-10-12', 60.00,  0.00, 'draft', 'Due on receipt', 'Daily',         'thon nguyen'),
    ('85009', '63706', '2026-10-12', 148.56, 1.23, 'paid',  'Due on receipt', 'With each job', 'Thọ Nguyễn Quang'),
    ('85000', '5001',  '2026-10-12', 65.00,  0.00, 'paid',  'Due on receipt', 'Daily',         'Thọ Nguyễn Quang'),
    ('85005', '63714', '2026-10-11', 152.26, 0.00, 'paid',  'Net 30 Terms',   'With each job', 'Thọ Nguyễn Quang'),
    ('85004', '63706', '2026-10-11', 138.63, 0.00, 'paid',  'Due on receipt', 'With each job', 'Thọ Nguyễn Quang'),
    ('84985', '5001',  '2026-10-11', 65.00,  0.00, 'paid',  'Due on receipt', 'Daily',         'Thọ Nguyễn Quang'),
    ('84979', '5001',  '2026-10-10', 65.00,  0.00, 'paid',  'Due on receipt', 'Daily',         'Thọ Nguyễn Quang'),
    ('84984', '63689', '2026-10-09', 95.00,  0.00, 'draft', 'Due on receipt', 'With each job', 'Thọ Nguyễn Quang'),
    ('84983', '63689', '2026-10-09', 95.00,  0.00, 'draft', 'Due on receipt', 'With each job', 'Thọ Nguyễn Quang'),
    ('84982', '63689', '2026-10-09', 95.00,  0.00, 'draft', 'Due on receipt', 'With each job', 'Thọ Nguyễn Quang'),
    ('84975', '5001',  '2026-10-09', 65.00,  0.00, 'paid',  'Due on receipt', 'Daily',         'Thọ Nguyễn Quang'),
    ('84890', '63689', '2026-09-24', 65.00,  0.00, 'draft', 'Due on receipt', 'Weekly',        'Thọ Nguyễn Quang')
) AS v(num, acct, inv_date, total, discount, status, terms, freq, creator)
JOIN customers c      ON c.account_number = v.acct
JOIN payment_terms pt ON pt.name = v.terms
JOIN users u          ON u.full_name = v.creator;

INSERT INTO invoice_tag_links (invoice_id, tag_id)
SELECT i.id, t.id
FROM (VALUES
    ('85030', 'a'), ('85030', 'b'), ('85030', 'c'),
    ('85028', 'a'), ('85028', 'b'), ('85028', 'c'),
    ('85005', 'a'), ('85005', 'b')
) AS v(num, tag)
JOIN invoices i     ON i.invoice_number = v.num
JOIN invoice_tags t ON t.name = v.tag;

-- Sent, archived and deleted invoices, so the report's summary and every tab have data.
-- Outstanding for the three 'sent' invoices is 95.00 + (100.00 - 30.00 paid) + 99.00 = 264.00.
INSERT INTO invoices (invoice_number, customer_id, job_id, billing_email, invoice_date, total, discount,
                      status, signature_status, payment_terms_id, frequency_label, created_by, archived_at, deleted_at)
SELECT v.num, c.id,
       (SELECT j.id FROM jobs j WHERE j.customer_id = c.id ORDER BY j.created_at DESC, j.id DESC LIMIT 1),
       c.billing_email, v.inv_date::date, v.total, 0.00,
       v.status::invoice_status, v.sig::signature_status, pt.id, 'With each job', u.id,
       v.archived_at::timestamptz, v.deleted_at::timestamptz
FROM (VALUES
    ('85031', '5001',  '2026-10-22', 95.00,  'sent', 'pending', NULL,         NULL),
    ('85032', '63706', '2026-10-20', 100.00, 'sent', 'signed',  NULL,         NULL),
    ('85033', '63689', '2026-10-18', 99.00,  'sent', 'draft',   NULL,         NULL),
    ('85034', '63714', '2026-10-17', 40.00,  'paid', 'signed',  '2026-10-18', NULL),
    ('85035', '63716', '2026-10-16', 25.00,  'draft', 'draft',  NULL,         '2026-10-17')
) AS v(num, acct, inv_date, total, status, sig, archived_at, deleted_at)
JOIN customers c      ON c.account_number = v.acct
JOIN payment_terms pt ON pt.name = 'Due on receipt'
JOIN users u          ON u.full_name = 'Thọ Nguyễn Quang';

-- Partial payment on 85032 (30.00 of 100.00).
INSERT INTO payments (customer_id, invoice_id, method_type, amount, payment_date)
SELECT i.customer_id, i.id, 'check', 30.00, '2026-10-21'
FROM invoices i
WHERE i.invoice_number = '85032';

-- Recurring invoices --------------------------------------------------------

INSERT INTO recurring_invoices (customer_id, frequency_unit, frequency_interval, weekday, occurrences,
                                action, status, total_value, started_on, next_invoice_id, next_invoice_date)
SELECT c.id, 'week', 1, 4, 3, 'send_by_email', 'active', 65.00, '2026-09-14', i.id, '2026-09-24'
FROM customers c
JOIN invoices i ON i.invoice_number = '84890'
WHERE c.account_number = '63689';

UPDATE invoices
SET recurring_invoice_id = (SELECT id FROM recurring_invoices ORDER BY id LIMIT 1)
WHERE invoice_number = '84890';

-- Estimates ----------------------------------------------------------------

INSERT INTO estimates (estimate_number, customer_id, billing_email, created_by, estimate_date, total)
SELECT v.num, c.id, c.billing_email, u.id, v.est_date::date, v.total
FROM (VALUES
    ('123187', '63733', '2026-10-20', 24.40, 'thon nguyen'),
    ('123184', '63717', '2026-10-20', 24.40, 'thon nguyen'),
    ('123186', '63733', '2026-10-19', 24.40, 'thon nguyen'),
    ('123185', '63717', '2026-10-19', 24.40, 'thon nguyen'),
    ('123183', '63714', '2026-10-19', 24.40, 'Thọ Nguyễn Quang'),
    ('123175', '63733', '2026-10-16', 24.40, 'Thọ Nguyễn Quang'),
    ('123174', '63733', '2026-10-16', 24.40, 'Thọ Nguyễn Quang'),
    ('123173', '63733', '2026-10-16', 24.40, 'Thọ Nguyễn Quang'),
    ('123172', '63714', '2026-10-14', 24.40, 'Thọ Nguyễn Quang'),
    ('123168', '63716', '2026-10-12', 24.40, 'thon nguyen'),
    ('123170', '63706', '2026-10-12', 24.40, 'Thọ Nguyễn Quang'),
    ('123169', '63730', '2026-10-12', 24.40, 'Thọ Nguyễn Quang'),
    ('123156', '63717', '2026-10-08', 0.00,  'Thọ Nguyễn Quang'),
    ('123159', '5002',  '2026-10-08', 96.22, 'Thọ Nguyễn Quang'),
    ('123158', '5002',  '2026-10-08', 3.00,  'Thọ Nguyễn Quang'),
    ('123157', '5002',  '2026-10-08', 0.00,  'Thọ Nguyễn Quang'),
    ('123155', '5001',  '2026-10-08', 80.60, 'thon nguyen')
) AS v(num, acct, est_date, total, creator)
JOIN customers c ON c.account_number = v.acct
JOIN users u     ON u.full_name = v.creator;

-- Documents ----------------------------------------------------------------

INSERT INTO documents (document_number, document_type_id, customer_id, created_by, created_at)
SELECT v.num, dt.id, c.id, u.id, v.created_at::timestamptz
FROM (VALUES
    ('5979', '1-Time Treatment (PestPros of MI)',            '63733', '2026-10-20 11:40:10', 'thon nguyen'),
    ('5978', '1-Time Treatment (PestPros of MI)',            '63733', '2026-10-20 09:49:06', 'thon nguyen'),
    ('5977', '1-Time Treatment (PestPros of MI)',            '63717', '2026-10-20 09:47:42', 'thon nguyen'),
    ('5972', '1-Time Treatment (PestPros of MI)',            '63717', '2026-10-20 00:58:53', 'thon nguyen'),
    ('5957', '1-Time Treatment (PestPros of MI)',            '63714', '2026-10-19 22:14:29', 'Thọ Nguyễn Quang'),
    ('5936', '1-Time Treatment (PestPros of MI)',            '63714', '2026-10-16 06:32:39', 'Thọ Nguyễn Quang'),
    ('5931', '1-Time Treatment (PestPros of MI)',            '63733', '2026-10-16 05:29:42', 'Thọ Nguyễn Quang'),
    ('5924', '1-Time Treatment (PestPros of MI)',            '63714', '2026-10-16 02:43:49', 'Thọ Nguyễn Quang'),
    ('5923', '1 Year SubTermite Agreement (LibertyPS)',      '63714', '2026-10-16 02:37:15', 'Thọ Nguyễn Quang'),
    ('5910', '1 Year Termite Warranty (Spidermen)',          '63706', '2026-10-15 05:22:38', 'Thọ Nguyễn Quang'),
    ('5859', '1-Time Treatment (PestPros of MI)',            '63706', '2026-10-12 02:55:27', 'Thọ Nguyễn Quang'),
    ('5805', '1 Year SubTermite Agreement (LibertyPS)',      '63706', '2026-10-07 07:31:04', 'Thọ Nguyễn Quang'),
    ('5751', '1 Year SubTermite Agreement (LibertyPS)',      '63706', '2026-10-05 06:21:44', 'Thọ Nguyễn Quang'),
    ('5746', '1 Year SubTermite Agreement (LibertyPS)',      '63706', '2026-10-05 04:41:13', 'Thọ Nguyễn Quang'),
    ('5743', '1 Year SubTermite Agreement (LibertyPS)',      '63714', '2026-10-05 02:39:02', 'Thọ Nguyễn Quang')
) AS v(num, doc_type, acct, created_at, creator)
JOIN document_types dt ON dt.name = v.doc_type
JOIN customers c       ON c.account_number = v.acct
JOIN users u           ON u.full_name = v.creator;

-- Credits and payments ------------------------------------------------------

INSERT INTO credits (customer_id, amount, method_type, method_detail, credit_date, created_by)
SELECT c.id, v.amount, v.method::payment_method_type, v.detail, '2026-10-15', u.id
FROM (VALUES
    ('63706', 100.00, 'check', 'Check'),
    ('63706', 200.00, 'card',  'Stripe **** 3222 Credit')
) AS v(acct, amount, method, detail)
JOIN customers c ON c.account_number = v.acct
JOIN users u     ON u.full_name = 'Thọ Nguyễn Quang';

-- Invoice payments (only for invoices that are paid).
INSERT INTO payments (customer_id, invoice_id, method_type, card_brand, card_last4, amount, payment_date)
SELECT i.customer_id, i.id, v.method::payment_method_type, v.brand, v.last4, i.total, v.pay_date::date
FROM (VALUES
    ('85009', 'cash',  NULL,     NULL,   '2026-10-15'),
    ('85004', 'card',  'Credit', '3222', '2026-10-15'),
    ('85010', 'card',  'Credit', '3222', '2026-10-15'),
    ('85011', 'card',  'Credit', '3222', '2026-10-15'),
    ('85000', 'check', NULL,     NULL,   '2026-10-12'),
    ('85005', 'card',  'Credit', '4242', '2026-10-11'),
    ('84985', 'check', NULL,     NULL,   '2026-10-11'),
    ('84979', 'check', NULL,     NULL,   '2026-10-10'),
    ('84975', 'check', NULL,     NULL,   '2026-10-08')
) AS v(num, method, brand, last4, pay_date)
JOIN invoices i ON i.invoice_number = v.num;

-- Credit "payments": the Payments report lists each credit as a row.
INSERT INTO payments (customer_id, credit_id, method_type, card_brand, card_last4, amount, payment_date)
SELECT cr.customer_id, cr.id, cr.method_type,
       CASE WHEN cr.method_type = 'card' THEN 'Credit' END,
       CASE WHEN cr.method_type = 'card' THEN '3222' END,
       cr.amount, cr.credit_date
FROM credits cr;
