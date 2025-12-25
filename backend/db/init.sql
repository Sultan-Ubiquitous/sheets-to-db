-- init.sql

CREATE DATABASE IF NOT EXISTS interndb;
USE interndb;

CREATE TABLE IF NOT EXISTS product (
    uuid VARCHAR(36) NOT NULL PRIMARY KEY,
    product_name VARCHAR(255) NOT NULL,
    quantity INT DEFAULT 0,
    price DECIMAL(10,2) NOT NULL,
    discount BOOLEAN DEFAULT FALSE,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    last_updated_by VARCHAR(50) DEFAULT 'system'
);

CREATE TABLE IF NOT EXISTS oauth_tokens (
    user_email VARCHAR(255) NOT NULL PRIMARY KEY,
    access_token TEXT NOT NULL,
    refresh_token TEXT,
    token_type VARCHAR(50),
    expiry DATETIME,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);


CREATE USER IF NOT EXISTS 'replicator'@'%' IDENTIFIED WITH mysql_native_password BY 'password';
GRANT ALL PRIVILEGES ON interndb.* TO 'replicator'@'%';
FLUSH PRIVILEGES;