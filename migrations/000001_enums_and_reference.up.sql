CREATE TYPE invoice_status AS ENUM ('draft', 'sent', 'paid', 'void');
CREATE TYPE signature_status AS ENUM ('draft', 'pending', 'signed', 'declined');
CREATE TYPE estimate_status AS ENUM ('draft', 'sent', 'accepted', 'declined');
CREATE TYPE document_status AS ENUM ('draft', 'sent', 'signed');
CREATE TYPE job_status AS ENUM ('unconfirmed', 'confirmed', 'completed', 'cancelled');
CREATE TYPE payment_status AS ENUM ('paid', 'pending', 'failed', 'refunded');
CREATE TYPE payment_method_type AS ENUM ('cash', 'check', 'card', 'other');
CREATE TYPE recurring_status AS ENUM ('active', 'paused', 'failed', 'ended');
CREATE TYPE recurring_action AS ENUM ('send_by_email', 'create_only');
CREATE TYPE frequency_unit AS ENUM ('day', 'week', 'month', 'year');

CREATE TABLE users (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    full_name text NOT NULL,
    email     text NOT NULL UNIQUE
);

CREATE TABLE lead_sources (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL UNIQUE
);

CREATE TABLE service_types (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL UNIQUE
);

CREATE TABLE payment_terms (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT ''
);

CREATE TABLE invoice_tags (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL UNIQUE
);

CREATE TABLE document_types (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL UNIQUE
);
