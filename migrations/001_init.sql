create table if not exists merchants (
    id text primary key,
    name text not null,
    home_country text not null default 'IN',
    created_at timestamptz not null default now()
);

create table if not exists payments (
    id text primary key,
    merchant_id text not null,
    user_id text not null,
    amount numeric(12,2) not null,
    currency text not null,
    payment_method text not null,
    device_id text not null,
    ip text not null,
    email text not null default '',
    phone text not null default '',
    billing_city text not null default '',
    billing_country text not null default '',
    card_hash text not null default '',
    status text not null default 'received',
    created_at timestamptz not null default now()
);

create table if not exists fraud_decisions (
    payment_id text primary key references payments(id) on delete cascade,
    decision text not null,
    risk_score integer not null,
    model_score integer not null,
    model_label text not null,
    triggered_rules jsonb not null,
    model_reason_codes jsonb not null,
    feature_snapshot jsonb not null,
    latency_ms bigint not null,
    created_at timestamptz not null default now()
);

create table if not exists device_links (
    device_id text not null,
    user_id text not null,
    seen_at timestamptz not null default now(),
    primary key (device_id, user_id)
);

create table if not exists card_links (
    card_hash text not null,
    user_id text not null,
    seen_at timestamptz not null default now(),
    primary key (card_hash, user_id)
);

create table if not exists ip_links (
    ip text not null,
    user_id text not null,
    seen_at timestamptz not null default now(),
    primary key (ip, user_id)
);
