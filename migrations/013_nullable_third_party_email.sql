-- Third-party providers may not return an email address. The email column is
-- nullable, and MySQL permits multiple NULL values in its unique index, while
-- an empty string can be stored only once.
ALTER TABLE users
MODIFY COLUMN email varchar(100) NULL DEFAULT NULL;

UPDATE users
SET email = NULL
WHERE email = '';
