BEGIN;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS chain_checkpoints;
DROP TABLE IF EXISTS chain_events;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS purchase_intents;
DROP TABLE IF EXISTS course_prices;
COMMIT;
