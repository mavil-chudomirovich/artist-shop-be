# Backend Specification

## Repository

artist-shop-backend

## Stack

* Go
* PostgreSQL
* REST API
* Docker

---

# Architecture

Pattern:

Modular Monolith

Structure:

backend/
├── cmd/
├── internal/
├── migrations/
├── pkg/
├── configs/

Modules:

* auth
* user
* product
* category
* inventory
* cart
* order
* payment
* shipping
* commission
* chat
* notification
* review
* wishlist
* content
* admin

---

# Authentication

Roles:

* CUSTOMER
* ADMIN

Features:

* Register
* Login
* Refresh token
* Logout
* Password reset

Security:

* Argon2id/Bcrypt
* JWT
* Refresh Token
* Rate limiting

---

# Database

Core tables:

users
addresses

products
categories
product_images

inventory_transactions

orders
order_items

payments
shipments

wishlists
wishlist_items

reviews

commissions
commission_quotes
commission_slots
commission_revisions
commission_files

conversations
messages

notifications

content_blocks

audit_logs

---

# API

Base URL:

/api/v1

Modules:

* Auth
* Product
* Cart
* Order
* Wishlist
* Review
* Commission
* Payment
* Notification

---

# Payment System

Providers:

* MoMo
* VietQR

Abstraction:

PaymentService
→ Provider
→ Result

Requirements:

* Idempotent webhooks
* Manual VietQR verification (MVP)
* Transaction-safe order processing

---

# Inventory

Automatic:

* Paid order → decrease stock
* Cancelled order → restore stock

Manual:

* Restock
* Damage
* Adjustment

Every change must create an InventoryTransaction record.

---

# Commission System

State Machine:

SUBMITTED
→ REVIEWING
→ QUOTED
→ WAITING_DEPOSIT
→ SLOT_RESERVED
→ WAITING_50_PAYMENT
→ IN_PROGRESS
→ PREVIEW
→ REVISION
→ APPROVED
→ WAITING_FINAL_PAYMENT
→ PAID_FULL
→ COMPLETED

---

# Chat

MVP:

REST + Polling

Optional:

WebSocket

Messages:

* Text
* Images
* Files
* System events

---

# Cloudinary

Uploads:

* Product images
* Commission references
* Commission previews
* Final artworks
* Avatars

Backend stores:

* public_id
* secure_url
* metadata

---

# Testing

Unit Tests:

* Pricing
* Inventory
* Order calculation
* Commission payment calculation
* Commission state transitions

API Tests:

* Auth
* Authorization
* Orders
* Payments
* Commissions
