package implement

import (
	"context"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
)

// The picture use cases are the module's one genuine use of the UnitOfWork port
// (research D6). The ten-picture ceiling is a count, which no unique index can
// express, so it is read while the product's row is locked. The provider call sits
// between two short transactions: the first refuses an eleventh picture before a
// byte is uploaded, and the second re-checks the count and inserts the row, which
// is what makes two concurrent uploads at the ceiling admit at most ten without
// holding a database lock across a network call.

// AddPicture validates the upload, stores it through the media port and audits the
// change (FR-016 to FR-020).
//
// The order is the specification:
//
//  1. The payload is decided on its own bytes, from the format signature and the
//     size ceiling, before anything is sent anywhere (FR-019).
//  2. A first transaction locks the product and refuses the upload when the
//     product already carries the maximum, so an eleventh picture is refused
//     before the provider is called and leaves no orphaned asset (FR-020).
//  3. The bytes go to the provider outside any transaction. Holding a database
//     lock across a third-party call is how a slow provider becomes a database
//     problem (research D6).
//  4. A second transaction re-checks the ceiling under the same lock and inserts
//     the row, so two concurrent uploads cannot both observe nine and both insert.
//     If the re-check refuses, the just-stored asset is released.
func (s *Service) AddPicture(ctx context.Context, in dto.AddPictureInput) (dto.AdminProductDetailOutput, error) {
	if err := model.ValidatePictureUpload(in.Content, s.Config.PictureMaxBytesOrDefault()); err != nil {
		return dto.AdminProductDetailOutput{}, err
	}

	picture, err := s.reservePicture(ctx, in.ProductID)
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}

	stored, err := s.Media.Upload(ctx, in.Content, s.Config.PictureTargetWidthOrDefault())
	if err != nil {
		// Whatever the provider said is dropped here: presentation logs whatever
		// error it is handed, and no provider text or credential belongs in a log
		// line (Constitution V, VI).
		return dto.AdminProductDetailOutput{}, domainerr.ErrProductMediaUnavailable
	}
	picture.PublicID = stored.PublicID
	picture.URL = stored.URL
	picture.Width = stored.Width
	picture.Height = stored.Height

	if err := s.insertPicture(ctx, picture); err != nil {
		// The row never went in, so the asset is ours to clean up. Failing the
		// request with the refusal while releasing keeps the product's storage
		// bounded (research D14).
		_ = s.Media.Remove(ctx, media.Reference{
			PublicID: stored.PublicID,
			URL:      stored.URL,
			Width:    stored.Width,
			Height:   stored.Height,
		})
		return dto.AdminProductDetailOutput{}, err
	}

	s.recordProduct(ctx, constant.AuditProductImageAdded, in.ProductID,
		map[string]any{"imageId": picture.ID.String()})
	return s.adminDetail(ctx, in.ProductID)
}

// reservePicture runs the pre-upload ceiling check: it locks the product, counts
// its pictures and refuses the upload when the product is already full. The
// returned picture carries the identifier, position and primary flag the insert
// will use; the position and flag are recomputed in insertPicture, so a concurrent
// insert cannot leave two main pictures.
func (s *Service) reservePicture(ctx context.Context, productID uuid.UUID) (*model.Picture, error) {
	var picture *model.Picture
	err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Products.LockProduct(txCtx, productID); err != nil {
			return err
		}
		pictures, err := s.Products.ListPictures(txCtx, productID)
		if err != nil {
			return err
		}
		if err := model.EnsurePictureCapacity(len(pictures)); err != nil {
			return err
		}
		picture = newPicture(productID, pictures)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return picture, nil
}

// insertPicture runs the count-and-insert inside one transaction, so two
// concurrent uploads at the ceiling cannot both admit (research D6). The position
// and the main-picture flag are recomputed from what is stored now, so the first
// picture becomes the main one even when an earlier upload committed meanwhile.
func (s *Service) insertPicture(ctx context.Context, picture *model.Picture) error {
	return s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Products.LockProduct(txCtx, picture.ProductID); err != nil {
			return err
		}
		count, err := s.Products.CountPictures(txCtx, picture.ProductID)
		if err != nil {
			return err
		}
		if err := model.EnsurePictureCapacity(count); err != nil {
			return err
		}
		pictures, err := s.Products.ListPictures(txCtx, picture.ProductID)
		if err != nil {
			return err
		}
		picture.Position = model.NextPicturePosition(pictures)
		picture.IsPrimary = len(pictures) == 0
		return s.Products.AddPicture(txCtx, picture)
	})
}

// RemovePicture removes a picture, promotes the next main one when needed,
// releases the stored asset after the row is gone and audits the change
// (FR-021, research D7, D14).
//
// A picture that does not belong to the product — or a product that does not
// exist — answers not-found, so the two are indistinguishable. The asset is
// released after the transaction commits: the row is the committed truth, so the
// worst case of a failed release is an orphaned asset rather than a product
// rendering a picture that no longer exists.
func (s *Service) RemovePicture(ctx context.Context, in dto.RemovePictureInput) error {
	var released model.Picture
	err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if err := s.Products.LockProduct(txCtx, in.ProductID); err != nil {
			return err
		}
		picture, err := s.Products.FindPicture(txCtx, in.ProductID, in.PictureID)
		if err != nil {
			return err
		}
		pictures, err := s.Products.ListPictures(txCtx, in.ProductID)
		if err != nil {
			return err
		}
		if err := s.Products.DeletePicture(txCtx, in.ProductID, in.PictureID); err != nil {
			return err
		}
		if next, ok := model.PromotionAfterRemoval(pictures, in.PictureID); ok {
			if err := s.Products.SetPrimaryPicture(txCtx, in.ProductID, next); err != nil {
				return err
			}
		}
		released = *picture
		return nil
	})
	if err != nil {
		return err
	}

	_ = s.Media.Remove(ctx, media.Reference{
		PublicID: released.PublicID,
		URL:      released.URL,
		Width:    released.Width,
		Height:   released.Height,
	})
	s.recordProduct(ctx, constant.AuditProductImageRemoved, in.ProductID,
		map[string]any{"imageId": in.PictureID.String()})
	return nil
}

// SetPrimaryPicture makes one picture the product's main one and audits the
// change (FR-017, research D7). It runs inside a transaction so the flag swap is
// atomic; the partial unique index is the guarantee that at most one picture is
// ever the main one. A picture that does not belong to the product answers
// not-found.
func (s *Service) SetPrimaryPicture(ctx context.Context, in dto.SetPrimaryPictureInput) (dto.AdminProductDetailOutput, error) {
	err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		// Locking the product row serialises two concurrent promotions of the
		// same product, so the two single-statement flag swaps cannot deadlock on
		// the picture rows (research D7).
		if err := s.Products.LockProduct(txCtx, in.ProductID); err != nil {
			return err
		}
		return s.Products.SetPrimaryPicture(txCtx, in.ProductID, in.PictureID)
	})
	if err != nil {
		return dto.AdminProductDetailOutput{}, err
	}
	s.recordProduct(ctx, constant.AuditProductImagePrimarySet, in.ProductID,
		map[string]any{"imageId": in.PictureID.String()})
	return s.adminDetail(ctx, in.ProductID)
}

// newPicture builds the picture a new upload becomes, before the provider has
// answered. The position and the primary flag are placeholders that insertPicture
// recomputes from the stored pictures; the identifier is fixed here so the audit
// entry and the response name the same picture.
func newPicture(productID uuid.UUID, existing []model.Picture) *model.Picture {
	return &model.Picture{
		ID:        uuid.New(),
		ProductID: productID,
		Position:  model.NextPicturePosition(existing),
		IsPrimary: len(existing) == 0,
		CreatedAt: now(),
	}
}
