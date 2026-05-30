ALTER TABLE dating_product_purchases
    DROP CONSTRAINT IF EXISTS dating_product_purchases_product_kind_chk;

ALTER TABLE dating_product_purchases
    ADD CONSTRAINT dating_product_purchases_product_kind_chk CHECK (
        product_kind IN ('subscription', 'spotlight', 'super_spotlight')
    );
