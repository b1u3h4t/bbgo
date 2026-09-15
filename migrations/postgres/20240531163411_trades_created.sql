-- +up
-- +begin
ALTER TABLE trades ADD COLUMN inserted_at TIMESTAMPTZ;
UPDATE trades SET inserted_at = traded_at;

CREATE OR REPLACE FUNCTION trades_set_inserted_at() RETURNS trigger AS $$
BEGIN
  IF NEW.inserted_at IS NULL THEN
    NEW.inserted_at := NOW();
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_inserted_at
BEFORE INSERT ON trades
FOR EACH ROW
EXECUTE PROCEDURE trades_set_inserted_at();
-- +end

-- +down
-- +begin
DROP TRIGGER IF EXISTS set_inserted_at ON trades;
DROP FUNCTION IF EXISTS trades_set_inserted_at();
ALTER TABLE trades DROP COLUMN IF EXISTS inserted_at;
-- +end
