// Package contracts holds interfaces published between modules. A module that
// needs another module's capability depends on an interface here, never on the
// other module's internals. The providing module supplies an adapter that
// satisfies the contract at the composition root.
//
// This package must not import any business module.
package contracts
