// Package repository declares the payment module's persistence port. The concrete
// implementation (infrastructure/implement/postgres) satisfies this interface. No
// repository method opens a transaction: the application layer owns the boundary
// through the UnitOfWork port (Constitution I).
package repository
