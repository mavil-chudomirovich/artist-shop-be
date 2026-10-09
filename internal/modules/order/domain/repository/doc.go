// Package repository declares the order module's persistence port. The concrete
// implementation (infrastructure/implement/postgres) embeds the generic
// share/repository.Base and satisfies this interface. No repository method opens a
// transaction: the application layer owns the boundary through the UnitOfWork
// port (Constitution I).
package repository
