-- Demo data for local runs and the simulator. Not a migration: customers must
-- never get these robots. Idempotent, so it can run on every start.
INSERT INTO sites (id, name) VALUES
    ('muc-office',  'Munich Office Tower'),
    ('muc-airport', 'Munich Airport T2'),
    ('ber-clinic',  'Berlin Clinic')
ON CONFLICT (id) DO NOTHING;

INSERT INTO robots (id, site_id, name, manufacturer, serial_number, model)
SELECT 'robot-' || lpad(n::text, 3, '0'),
       (ARRAY['muc-office', 'muc-airport', 'ber-clinic'])[1 + (n - 1) % 3],
       (ARRAY['Cleaner', 'Courier', 'Guide'])[1 + (n - 1) / 3 % 3] || ' ' || n,
       (ARRAY['acme', 'nordbot', 'servio'])[1 + (n - 1) / 8 % 3],
       upper((ARRAY['acme', 'nordbot', 'servio'])[1 + (n - 1) / 8 % 3]) || '-' || lpad(n::text, 4, '0'),
       (ARRAY['C-200', 'D-10', 'G-5'])[1 + (n - 1) / 3 % 3]
-- sites rotate every robot, robot types every 3, manufacturers every 8
FROM generate_series(1, 24) AS n
ON CONFLICT (id) DO NOTHING;
