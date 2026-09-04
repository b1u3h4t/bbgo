-- +up
-- +begin
CREATE TABLE IF NOT EXISTS `binance_futures_klines` LIKE `binance_klines`;
-- +end

-- +begin
ALTER TABLE `binance_futures_klines` MODIFY `symbol` VARCHAR(32) NOT NULL;
-- +end

-- +begin
CREATE TABLE IF NOT EXISTS `binance_delivery_klines` LIKE `binance_klines`;
-- +end

-- +begin
ALTER TABLE `binance_delivery_klines` MODIFY `symbol` VARCHAR(32) NOT NULL;
-- +end

-- +down

-- +begin
DROP TABLE IF EXISTS `binance_delivery_klines`;
-- +end

-- +begin
DROP TABLE IF EXISTS `binance_futures_klines`;
-- +end
