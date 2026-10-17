-- A migration is applied once, in file order. Never edit one that has been applied: add the next.
CREATE TABLE app_info (
    key   text PRIMARY KEY,
    value text NOT NULL
);
