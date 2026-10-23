-- Development sample data mirroring the report screenshots (October 2026).
-- WARNING: truncates every data table first. Never run against production.
-- Rows reference each other by natural keys (account/invoice/estimate numbers),
-- so the script does not depend on identity values.

TRUNCATE payments, credits, documents, invoice_tag_links, recurring_invoices,
         billing_documents, jobs, service_locations, customer_phones, customers,
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
SELECT v.acct, v.first_name, v.last_name, v.display_name, v.email, v.email, ls.id, v.top_note, v.created_at::timestamp AT TIME ZONE 'UTC'
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
SELECT c.id, l.id, st.id, ls.id, v.status::job_status, v.created_at::timestamp AT TIME ZONE 'UTC', v.value,
       v.created_at::timestamp AT TIME ZONE 'UTC'
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
FROM generate_series('2026-10-09 08:00+00'::timestamptz, '2026-10-15 08:00+00'::timestamptz, interval '1 day') AS d
CROSS JOIN customers c
JOIN service_locations l ON l.customer_id = c.id
CROSS JOIN service_types st
WHERE c.account_number = '5001' AND st.name = 'Bi-Monthly Service';

-- Invoices ----------------------------------------------------------------
-- job_id links to the customer's most recent job, when one exists.

INSERT INTO billing_documents (parent_type, document_number, customer_id, job_id, billing_email, document_date,
                               total, discount, status, signature_status, payment_terms_id, frequency_label,
                               created_by)
SELECT 'invoice', v.num, c.id,
       (SELECT j.id FROM jobs j WHERE j.customer_id = c.id ORDER BY j.created_at DESC, j.id DESC LIMIT 1),
       c.billing_email, v.inv_date::date, v.total, v.discount,
       v.status::billing_status, 'draft', pt.id, v.freq, u.id
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
JOIN billing_documents i ON i.parent_type = 'invoice' AND i.document_number = v.num
JOIN invoice_tags t      ON t.name = v.tag;

-- Sent, archived and deleted invoices, so the report's summary and every tab have data.
-- Outstanding for the three 'sent' invoices is 95.00 + (100.00 - 30.00 paid) + 99.00 = 264.00.
INSERT INTO billing_documents (parent_type, document_number, customer_id, job_id, billing_email, document_date,
                               total, discount, status, signature_status, payment_terms_id, frequency_label,
                               created_by, archived_at, deleted_at)
SELECT 'invoice', v.num, c.id,
       (SELECT j.id FROM jobs j WHERE j.customer_id = c.id ORDER BY j.created_at DESC, j.id DESC LIMIT 1),
       c.billing_email, v.inv_date::date, v.total, 0.00,
       v.status::billing_status, v.sig::signature_status, pt.id, 'With each job', u.id,
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
FROM billing_documents i
WHERE i.parent_type = 'invoice' AND i.document_number = '85032';

-- Recurring invoices --------------------------------------------------------

INSERT INTO recurring_invoices (customer_id, frequency_unit, frequency_interval, weekday, occurrences,
                                action, status, total_value, started_on, next_invoice_id, next_invoice_date)
SELECT c.id, 'week', 1, 4, 3, 'send_by_email', 'active', 65.00, '2026-09-14', i.id, '2026-09-24'
FROM customers c
JOIN billing_documents i ON i.parent_type = 'invoice' AND i.document_number = '84890'
WHERE c.account_number = '63689';

UPDATE billing_documents
SET recurring_invoice_id = (SELECT id FROM recurring_invoices ORDER BY id LIMIT 1)
WHERE parent_type = 'invoice' AND document_number = '84890';

-- Estimates ----------------------------------------------------------------

INSERT INTO billing_documents (parent_type, document_number, customer_id, billing_email, created_by,
                               document_date, total)
SELECT 'estimate', v.num, c.id, c.billing_email, u.id, v.est_date::date, v.total
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

-- Estimates in other states, so every status and tab of the Estimates report has data.
INSERT INTO billing_documents (parent_type, document_number, customer_id, billing_email, created_by,
                               document_date, total, status, signature_status, archived_at, deleted_at)
SELECT 'estimate', v.num, c.id, c.billing_email, u.id, v.est_date::date, v.total,
       v.status::billing_status, v.sig::signature_status, v.archived_at::timestamptz, v.deleted_at::timestamptz
FROM (VALUES
    ('123188', '63733', '2026-10-21', 150.00, 'accepted', 'signed',   NULL,         NULL),
    ('123189', '63717', '2026-10-21', 75.50,  'declined', 'declined', NULL,         NULL),
    ('123150', '63714', '2026-10-05', 40.00,  'sent',     'pending',  '2026-10-06', NULL),
    ('123151', '63706', '2026-10-04', 10.00,  'draft',    'draft',    NULL,         '2026-10-05')
) AS v(num, acct, est_date, total, status, sig, archived_at, deleted_at)
JOIN customers c ON c.account_number = v.acct
JOIN users u     ON u.full_name = 'Thọ Nguyễn Quang';

-- Documents ----------------------------------------------------------------

INSERT INTO documents (document_number, document_type_id, customer_id, created_by, created_at)
SELECT v.num, dt.id, c.id, u.id, v.created_at::timestamp AT TIME ZONE 'UTC'
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

-- Documents in other states, so every status, type and tab of the Documents report has data.
INSERT INTO documents (document_number, document_type_id, customer_id, created_by, status, created_at, deleted_at)
SELECT v.num, dt.id, c.id, u.id, v.status::document_status, v.created_at::timestamp AT TIME ZONE 'UTC', v.deleted_at::timestamp AT TIME ZONE 'UTC'
FROM (VALUES
    ('5980', '1 Year Termite Warranty (Spidermen)',       '63706', 'sent',   '2026-10-21 10:00:00',        NULL),
    ('5981', '1 Year SubTermite Agreement (LibertyPS)',   '63714', 'signed', '2026-10-21 12:30:00',        NULL),
    ('5982', '1-Time Treatment (PestPros of MI)',         '63733', 'signed', '2026-10-22 08:15:30.123456', NULL),
    ('5900', '1-Time Treatment (PestPros of MI)',         '63717', 'draft',  '2026-10-10 09:00:00',        '2026-10-18 09:00:00')
) AS v(num, doc_type, acct, status, created_at, deleted_at)
JOIN document_types dt ON dt.name = v.doc_type
JOIN customers c       ON c.account_number = v.acct
JOIN users u           ON u.full_name = 'Thọ Nguyễn Quang';

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
JOIN billing_documents i ON i.parent_type = 'invoice' AND i.document_number = v.num;

-- Credit "payments": the Payments report lists each credit as a row.
INSERT INTO payments (customer_id, credit_id, method_type, card_brand, card_last4, amount, payment_date)
SELECT cr.customer_id, cr.id, cr.method_type,
       CASE WHEN cr.method_type = 'card' THEN 'Credit' END,
       CASE WHEN cr.method_type = 'card' THEN '3222' END,
       cr.amount, cr.credit_date
FROM credits cr;

-- Payments in other states, for the Payments report: every status, the "other" method, a second card
-- brand, a payment outside October and a deleted one. They sit on invoices that are already paid, so no
-- outstanding balance changes. Several share a date to exercise paging ties.
INSERT INTO payments (customer_id, invoice_id, method_type, card_brand, card_last4, amount, payment_date,
                      status, deleted_at)
SELECT i.customer_id, i.id, v.method::payment_method_type, v.brand, v.last4, v.amount, v.pay_date::date,
       v.status::payment_status, v.deleted_at::timestamptz
FROM (VALUES
    ('85009', 'other', NULL,    NULL,   12.50, '2026-10-16', 'pending',  NULL),
    ('85004', 'card',  'Visa',  '4242', 20.00, '2026-10-15', 'failed',   NULL),
    ('85010', 'card',  'Debit', '5555', 15.00, '2026-10-15', 'refunded', NULL),
    ('85000', 'check', NULL,    NULL,   10.00, '2026-09-30', 'paid',     NULL),
    ('85005', 'cash',  NULL,    NULL,    5.00, '2026-10-11', 'paid',     '2026-10-12 09:00:00+00')
) AS v(num, method, brand, last4, amount, pay_date, status, deleted_at)
JOIN billing_documents i ON i.parent_type = 'invoice' AND i.document_number = v.num;

-- More credits for the Credits report: every method, an empty detail, no creator, another customer, a credit
-- outside October and several on one date (paging ties). They are added after the "credit payments" insert
-- above on purpose, so they stay out of the Payments report.
INSERT INTO credits (customer_id, amount, method_type, method_detail, credit_date, created_by)
SELECT c.id, v.amount, v.method::payment_method_type, v.detail, v.credit_date::date, u.id
FROM (VALUES
    ('63714',  50.00, 'cash',  'Cash at the door',      '2026-10-15', 'thon nguyen'),
    ('63714',  75.25, 'check', 'Check #1042',           '2026-10-12', 'Thọ Nguyễn Quang'),
    ('63733', 120.00, 'other', 'Wire transfer',         '2026-10-21', NULL),
    ('63733',  20.00, 'card',  'Stripe **** 4242 Visa', '2026-10-15', 'thon nguyen'),
    ('63717',  10.00, 'cash',  '',                      '2026-10-15', 'Thọ Nguyễn Quang'),
    ('63717',  35.50, 'check', 'Check #77',             '2026-09-30', 'thon nguyen')
) AS v(acct, amount, method, detail, credit_date, creator)
JOIN customers c ON c.account_number = v.acct
LEFT JOIN users u ON u.full_name = v.creator;

-- More recurring invoices for the Recurring Invoices report: every status and action, every repeat unit, a
-- missing weekday / occurrence count / end date, a due date without an invoice yet, a next invoice, and two
-- that share a start date (paging ties). They are not linked from billing_documents, so the Invoices report
-- is unchanged.
INSERT INTO recurring_invoices (customer_id, frequency_unit, frequency_interval, weekday, occurrences,
                                action, status, total_value, started_on, ends_on, next_invoice_id, next_invoice_date)
SELECT c.id, v.unit::frequency_unit, v.every, v.weekday, v.occurrences, v.action::recurring_action,
       v.status::recurring_status, v.total, v.started::date, v.ends::date, i.id, v.next_date::date
FROM (VALUES
    ('63714', 'month', 1, NULL::smallint, NULL::int, 'create_only',   'paused', 120.00, '2026-08-03', NULL,         NULL,    NULL),
    ('63733', 'week',  2, 2::smallint,    6,         'send_by_email', 'failed', 48.50,  '2026-10-05', NULL,         '85032', '2026-10-19'),
    ('63717', 'year',  1, NULL::smallint, NULL::int, 'send_by_email', 'ended',  300.00, '2025-10-01', '2026-10-01', NULL,    NULL),
    ('63706', 'day',   3, NULL::smallint, 10,        'create_only',   'active', 15.00,  '2026-10-15', NULL,         NULL,    '2026-10-18'),
    ('63706', 'week',  1, 4::smallint,    NULL::int, 'send_by_email', 'active', 65.00,  '2026-09-14', NULL,         NULL,    '2026-09-24'),
    ('63733', 'month', 2, NULL::smallint, NULL::int, 'send_by_email', 'active', 99.99,  '2026-07-01', '2026-12-31', NULL,    '2026-11-01')
) AS v(acct, unit, every, weekday, occurrences, action, status, total, started, ends, next_number, next_date)
JOIN customers c ON c.account_number = v.acct
LEFT JOIN billing_documents i ON i.parent_type = 'invoice' AND i.document_number = v.next_number;

-- More jobs for the New Jobs report: a cancelled job (none above), every lead source, a microsecond timestamp,
-- the first and the last instant of a UTC day, jobs just outside October (09-30 and 11-01) and two created at the
-- very same instant (paging ties). Timestamps are explicit UTC so the data does not depend on the database's time
-- zone. They are added after the invoices on purpose: an invoice links to its customer's most recent job at the
-- time it is created, so these do not change the Invoices report.
INSERT INTO lead_sources (name) VALUES ('Google'), ('Referral');

INSERT INTO jobs (customer_id, location_id, service_type_id, lead_source_id, status, scheduled_at, invoice_value, created_at)
SELECT c.id, l.id, st.id, ls.id, v.status::job_status, v.created_at::timestamptz, v.value, v.created_at::timestamptz
FROM (VALUES
    ('63733', 'Initial Service',    'cancelled',   '2026-10-22 08:15:30.123456+00', 95.00, 'Google'),
    ('63714', 'Full service',       'completed',   '2026-10-10 00:00:00+00',        79.30, 'Referral'),
    ('63717', 'Call Back Service',  'confirmed',   '2026-10-10 23:59:59.999999+00', 10.33, 'Facebook'),
    ('63706', 'Bi-Monthly Service', 'completed',   '2026-09-30 23:00:00+00',        65.00, NULL),
    ('63706', 'Bi-Monthly Service', 'unconfirmed', '2026-11-01 00:00:00+00',        65.00, 'Google'),
    ('63733', 'Blank Service 123',  'confirmed',   '2026-10-12 06:00:00+00',         0.00, NULL),
    ('63714', 'Blank Service 123',  'confirmed',   '2026-10-12 06:00:00+00',         0.00, 'Google'),
    ('5001',  'Bi-Monthly Service', 'cancelled',   '2026-10-13 09:30:00+00',        65.00, NULL)
) AS v(acct, service, status, created_at, value, source)
JOIN customers c          ON c.account_number = v.acct
JOIN service_locations l  ON l.customer_id = c.id
JOIN service_types st     ON st.name = v.service
LEFT JOIN lead_sources ls ON ls.name = v.source;

-- More customers for the New Customers report: a microsecond timestamp, the first and the last instant of a UTC
-- day, customers just outside October (09-30 and 11-01), two created at the very same instant (paging ties),
-- every lead source, and customers with and without an email and a phone (one has two phones: the report shows
-- the first). Timestamps are explicit UTC so the data does not depend on the database's time zone. They are
-- added last, so nothing above changes.
INSERT INTO customers (account_number, first_name, last_name, display_name, email, billing_email, lead_source_id, created_at)
SELECT v.acct, '', v.display_name, v.display_name, v.email, v.email, ls.id, v.created_at::timestamptz
FROM (VALUES
    ('70001', 'Ada Lovelace',     'ada@example.com',    'Google',   '2026-10-22 08:15:30.123456+00'),
    ('70002', 'Boundary Start',   NULL,                 NULL,       '2026-10-10 00:00:00+00'),
    ('70003', 'Boundary End',     'end@example.com',    'Referral', '2026-10-10 23:59:59.999999+00'),
    ('70004', 'September Last',   'sept@example.com',   'Google',   '2026-09-30 23:00:00+00'),
    ('70005', 'November First',   NULL,                 NULL,       '2026-11-01 00:00:00+00'),
    ('70006', 'Tie One',          'tie@example.com',    'Facebook', '2026-10-12 06:00:00+00'),
    ('70007', 'Tie Two',          NULL,                 'Facebook', '2026-10-12 06:00:00+00'),
    ('70008', 'Percent 50% Off',  'a_b@example.com',    NULL,       '2026-10-13 09:30:00+00')
) AS v(acct, display_name, email, source, created_at)
LEFT JOIN lead_sources ls ON ls.name = v.source;

INSERT INTO customer_phones (customer_id, number, kind)
SELECT c.id, v.number, 'mobile'
FROM (VALUES
    (1, '70001', '555-0101'),
    (2, '70001', '555-0102'),
    (3, '70002', '555-0202'),
    (4, '70006', '555-0606'),
    (5, '70007', '555-0707')
) AS v(n, acct, number)
JOIN customers c ON c.account_number = v.acct
ORDER BY v.n;
