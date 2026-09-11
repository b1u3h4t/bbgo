-- +up
-- +begin
ALTER TABLE `orders`
    CHANGE `order_type` `order_type` varchar(32) NOT NULL;
-- +end

-- +down

-- +begin
ALTER TABLE `orders`
    CHANGE `order_type` `order_type` varchar(16) NOT NULL;
-- +end
