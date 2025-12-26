-- init.sql

CREATE DATABASE IF NOT EXISTS interndb;
USE interndb;

CREATE TABLE IF NOT EXISTS product (
    uuid VARCHAR(36) NOT NULL PRIMARY KEY,
    product_name VARCHAR(255) DEFAULT 'Untitled',  
    quantity INT DEFAULT 0,
    price DECIMAL(10,2) DEFAULT 0.00,              
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

CREATE TABLE IF NOT EXISTS sheet_mappings (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE, 
    spreadsheet_id VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE USER IF NOT EXISTS 'replicator'@'%' IDENTIFIED WITH mysql_native_password BY 'password';
GRANT REPLICATION SLAVE, REPLICATION CLIENT, SELECT ON *.* TO 'replicator'@'%';
FLUSH PRIVILEGES;

INSERT IGNORE INTO product (uuid, product_name, quantity, price, discount) VALUES
('u-101', 'Gaming Mouse', 50, 49.99, FALSE),
('u-102', 'Mechanical Keyboard', 30, 120.00, TRUE),
('u-103', 'USB-C Cable', 100, 9.99, FALSE);