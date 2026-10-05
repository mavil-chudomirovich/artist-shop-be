// Package repository declares the user module's repository ports. Concrete
// implementations (infrastructure/implement/postgres) embed the generic
// share/repository.Base and satisfy these interfaces.
//
// A repository MUST NOT open a transaction: the application layer owns the
// boundary through the UnitOfWork port.
package repository
