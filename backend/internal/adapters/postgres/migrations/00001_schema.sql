-- +goose Up

-- Registry: changes rarely, protected by constraints.
CREATE TABLE sites (
    id   text PRIMARY KEY,
    name text NOT NULL
);

CREATE TABLE robots (
    id            text PRIMARY KEY,
    site_id       text NOT NULL REFERENCES sites (id),
    name          text NOT NULL,
    manufacturer  text NOT NULL,
    serial_number text NOT NULL,
    model         text NOT NULL DEFAULT '',
    UNIQUE (manufacturer, serial_number) -- the robot's own identity
);
CREATE INDEX robots_site_id_idx ON robots (site_id);

-- Current state: one row per robot, replaced only by a newer report.
-- No indexes besides the key, so frequent updates stay cheap (HOT updates).
CREATE TABLE robot_state (
    robot_id        text PRIMARY KEY REFERENCES robots (id),
    reported_at     timestamptz      NOT NULL, -- robot clock: ordering
    received_at     timestamptz      NOT NULL, -- server clock: liveness
    map_id          text             NOT NULL,
    x               double precision NOT NULL,
    y               double precision NOT NULL,
    theta           double precision NOT NULL,
    localized       boolean          NOT NULL,
    state_of_charge double precision NOT NULL CHECK (state_of_charge BETWEEN 0 AND 100),
    charging        boolean          NOT NULL,
    mode            text             NOT NULL,
    faults          jsonb            NOT NULL DEFAULT '[]',
    emergency_stop  text             NOT NULL,
    field_violation boolean          NOT NULL,
    vendor_data     jsonb
);

-- History: append-only. The primary key doubles as the dedup rule and as the
-- index for "one robot over a time range". At scale: partition by reported_at.
CREATE TABLE telemetry (
    robot_id        text             NOT NULL REFERENCES robots (id),
    reported_at     timestamptz      NOT NULL,
    received_at     timestamptz      NOT NULL,
    map_id          text             NOT NULL,
    x               double precision NOT NULL,
    y               double precision NOT NULL,
    theta           double precision NOT NULL,
    localized       boolean          NOT NULL,
    state_of_charge double precision NOT NULL CHECK (state_of_charge BETWEEN 0 AND 100),
    charging        boolean          NOT NULL,
    mode            text             NOT NULL,
    faults          jsonb            NOT NULL DEFAULT '[]',
    emergency_stop  text             NOT NULL,
    field_violation boolean          NOT NULL,
    vendor_data     jsonb,
    PRIMARY KEY (robot_id, reported_at)
);

-- +goose Down
DROP TABLE telemetry;
DROP TABLE robot_state;
DROP TABLE robots;
DROP TABLE sites;
