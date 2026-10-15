package implement

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/application/dto"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/constant"
	domainerr "github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/error"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/modules/product/domain/model"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/audit"
	"github.com/mavil-chudomirovich/artist-shop-be/internal/share/media"
)

// targetTypeVariantGroup is the audit_logs target type of the variant-group
// events. The target of a group mutation is the group itself, so the audit index
// on (target_type, target_id, occurred_at) reconstructs what happened to one
// group without scanning the actor's other events (FR-014, Constitution VI).
const targetTypeVariantGroup = "variant_group"

// The variant-group use cases (FR-009 to FR-015, SC-006).
//
// A group is a display container: it has no price, no stock and no sell state, and
// it is never bought. The group's own picture gallery follows the same media and
// UnitOfWork pattern a product's does (research D2); the ten-picture ceiling is a
// count, which no unique index can express, so it is read inside the caller's
// transaction before the provider is called. Every mutation emits its audit event
// naming the group and the acting administrator, and no event carries a customer's
// personal data.

// CreateVariantGroup creates a group and audits the change (FR-009, FR-014).
//
// The entity validates the name, the slug, the description and the choice-dimension
// name before anything is written, so a rejected draft persists nothing and names
// the offending member (FR-020). A slug another group already holds is reported by
// the repository as the collision sentinel, and a category identifier no row
// carries as the invalid-field sentinel naming categoryId (FR-019, FR-023); both
// reach the caller through presentation's mapping.
//
// The acting administrator is taken from the context the session filled, never
// from the input (FR-015).
func (s *Service) CreateVariantGroup(ctx context.Context, in dto.CreateVariantGroupInput) (dto.AdminVariantGroupDetailOutput, error) {
	group, err := model.NewVariantGroup(model.VariantGroupDraft{
		Name:        in.Name,
		Slug:        in.Slug,
		Description: in.Description,
		CategoryID:  in.CategoryID,
		Position:    in.Position,
		OptionName:  in.OptionName,
	}, now())
	if err != nil {
		return dto.AdminVariantGroupDetailOutput{}, err
	}
	// A single group row needs no transaction: the repository's Create is one
	// statement, so either the row is there or it is not (Constitution II owns
	// money and stock, which a group has none of).
	if err := s.Groups.Create(ctx, group); err != nil {
		return dto.AdminVariantGroupDetailOutput{}, err
	}
	s.recordGroup(ctx, constant.AuditVariantGroupCreated, group.ID, map[string]any{"slug": group.Slug})
	return s.adminGroupDetail(ctx, group.ID)
}

// ListAdminGroups returns one page of every group, including the ones withheld
// from customers (FR-013). It applies the same window the product list applies, so
// an out-of-range request is clamped rather than refused.
func (s *Service) ListAdminGroups(ctx context.Context, in dto.ListAdminGroupsInput) (dto.AdminVariantGroupPage, error) {
	page, pageSize := listWindow(in.Page, in.PageSize)
	items, total, err := s.Groups.ListAll(ctx, page, pageSize)
	if err != nil {
		return dto.AdminVariantGroupPage{}, err
	}
	return dto.AdminVariantGroupPage{
		Groups:   s.Mapper.AdminVariantGroups(items),
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

// GetAdminGroup returns one group by identifier, including one not visible to
// customers, with its own pictures and every option, visible or not (FR-013). An
// unknown identifier is reported as the module's not-found.
func (s *Service) GetAdminGroup(ctx context.Context, in dto.AdminVariantGroupRefInput) (dto.AdminVariantGroupDetailOutput, error) {
	return s.adminGroupDetail(ctx, in.ID)
}

// UpdateVariantGroup applies a partial edit and audits the change (FR-011, FR-014).
//
// An omitted member keeps its current value. The entity validates the whole change
// before writing any of it, so a rejection leaves the row untouched (FR-020). The
// pictures are edited through their own operations, so they are deliberately absent
// here.
func (s *Service) UpdateVariantGroup(ctx context.Context, in dto.UpdateVariantGroupInput) (dto.AdminVariantGroupDetailOutput, error) {
	view, err := s.Groups.FindByID(ctx, in.ID)
	if err != nil {
		return dto.AdminVariantGroupDetailOutput{}, err
	}
	group := view.Group
	if err := group.Apply(model.VariantGroupEdit{
		Name:        in.Name,
		Slug:        in.Slug,
		Description: in.Description,
		CategoryID:  in.CategoryID,
		Position:    in.Position,
		OptionName:  in.OptionName,
	}, now()); err != nil {
		return dto.AdminVariantGroupDetailOutput{}, err
	}
	if err := s.Groups.Update(ctx, &group); err != nil {
		return dto.AdminVariantGroupDetailOutput{}, err
	}
	s.recordGroup(ctx, constant.AuditVariantGroupUpdated, group.ID, map[string]any{"slug": group.Slug})
	return s.adminGroupDetail(ctx, in.ID)
}

// DeleteVariantGroup removes a group and audits the removal (FR-012, FR-014).
//
// Removal is refused while options remain: the repository reports the restricting
// foreign key's refusal as the group-with-options sentinel, so the operator
// detaches the options first (FR-025, research D10). On success the group's own
// pictures cascade away and no product is touched. An unknown identifier — including
// one already removed — is the module's not-found.
func (s *Service) DeleteVariantGroup(ctx context.Context, in dto.AdminVariantGroupRefInput) error {
	if err := s.Groups.Delete(ctx, in.ID); err != nil {
		return err
	}
	s.recordGroup(ctx, constant.AuditVariantGroupDeleted, in.ID, nil)
	return nil
}

// AddGroupImage validates the upload, stores it through the media port and audits
// the change (FR-018).
//
// The order mirrors the product picture use case: the payload is decided on its own
// bytes, a first transaction refuses an eleventh picture before a byte is uploaded,
// the bytes go to the provider outside any transaction, and a second transaction
// re-checks the ceiling and inserts the row. If the re-check refuses, the
// just-stored asset is released.
func (s *Service) AddGroupImage(ctx context.Context, in dto.AddGroupImageInput) (dto.AdminVariantGroupDetailOutput, error) {
	if err := model.ValidatePictureUpload(in.Content, s.Config.PictureMaxBytesOrDefault()); err != nil {
		return dto.AdminVariantGroupDetailOutput{}, err
	}

	image, err := s.reserveGroupImage(ctx, in.GroupID)
	if err != nil {
		return dto.AdminVariantGroupDetailOutput{}, err
	}

	stored, err := s.Media.Upload(ctx, in.Content, s.Config.PictureTargetWidthOrDefault())
	if err != nil {
		// Whatever the provider said is dropped here: presentation logs whatever
		// error it is handed, and no provider text or credential belongs in a log
		// line (Constitution V, VI).
		return dto.AdminVariantGroupDetailOutput{}, domainerr.ErrProductMediaUnavailable
	}
	image.PublicID = stored.PublicID
	image.URL = stored.URL
	image.Width = stored.Width
	image.Height = stored.Height

	if err := s.insertGroupImage(ctx, image); err != nil {
		// The row never went in, so the asset is ours to clean up (research D14).
		_ = s.Media.Remove(ctx, media.Reference{
			PublicID: stored.PublicID,
			URL:      stored.URL,
			Width:    stored.Width,
			Height:   stored.Height,
		})
		return dto.AdminVariantGroupDetailOutput{}, err
	}

	s.recordGroup(ctx, constant.AuditVariantGroupImageAdded, in.GroupID,
		map[string]any{"imageId": image.ID.String()})
	return s.adminGroupDetail(ctx, in.GroupID)
}

// reserveGroupImage runs the pre-upload check: it confirms the group exists, counts
// its pictures and refuses the upload when the gallery is already full. It pulls no
// group row lock — the group port exposes a count rather than a lock (research D2) —
// so the ceiling is re-checked under the insert transaction below.
func (s *Service) reserveGroupImage(ctx context.Context, groupID uuid.UUID) (*model.VariantGroupImage, error) {
	var image *model.VariantGroupImage
	err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		if _, err := s.Groups.FindByID(txCtx, groupID); err != nil {
			return err
		}
		count, err := s.Groups.CountImages(txCtx, groupID)
		if err != nil {
			return err
		}
		if err := model.EnsurePictureCapacity(count); err != nil {
			return err
		}
		images, err := s.Groups.ListImages(txCtx, groupID)
		if err != nil {
			return err
		}
		image = newGroupImage(groupID, images)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return image, nil
}

// insertGroupImage runs the count-and-insert inside one transaction, so a second
// concurrent upload at the ceiling is refused rather than admitted. The position and
// the main-picture flag are recomputed from what is stored now, so the first picture
// becomes the main one even when an earlier upload committed meanwhile.
func (s *Service) insertGroupImage(ctx context.Context, image *model.VariantGroupImage) error {
	return s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		count, err := s.Groups.CountImages(txCtx, image.GroupID)
		if err != nil {
			return err
		}
		if err := model.EnsurePictureCapacity(count); err != nil {
			return err
		}
		images, err := s.Groups.ListImages(txCtx, image.GroupID)
		if err != nil {
			return err
		}
		image.Position = nextGroupImagePosition(images)
		image.IsPrimary = len(images) == 0
		return s.Groups.AddImage(txCtx, image)
	})
}

// RemoveGroupImage removes a group picture, promotes the next main one when needed,
// releases the stored asset after the row is gone and audits the change (FR-018).
//
// A picture that does not belong to the group — or a group that does not exist —
// answers not-found, so the two are indistinguishable. The asset is released after
// the transaction commits: the row is the committed truth, so the worst case of a
// failed release is an orphaned asset rather than a group rendering a picture that
// no longer exists.
func (s *Service) RemoveGroupImage(ctx context.Context, in dto.RemoveGroupImageInput) error {
	var released model.VariantGroupImage
	err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		image, err := s.Groups.FindImage(txCtx, in.GroupID, in.ImageID)
		if err != nil {
			return err
		}
		images, err := s.Groups.ListImages(txCtx, in.GroupID)
		if err != nil {
			return err
		}
		if err := s.Groups.DeleteImage(txCtx, in.GroupID, in.ImageID); err != nil {
			return err
		}
		if next, ok := groupImagePromotionAfterRemoval(images, in.ImageID); ok {
			if err := s.Groups.SetPrimaryImage(txCtx, in.GroupID, next); err != nil {
				return err
			}
		}
		released = *image
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
	s.recordGroup(ctx, constant.AuditVariantGroupImageRemoved, in.GroupID,
		map[string]any{"imageId": in.ImageID.String()})
	return nil
}

// SetPrimaryGroupImage makes one picture the group's main one and audits the change
// (FR-018). It runs inside a transaction so the flag swap is atomic; the partial
// unique index is the guarantee that at most one picture is ever the main one. A
// picture that does not belong to the group answers not-found.
func (s *Service) SetPrimaryGroupImage(ctx context.Context, in dto.SetPrimaryGroupImageInput) (dto.AdminVariantGroupDetailOutput, error) {
	err := s.Tx.WithinTx(ctx, func(txCtx context.Context) error {
		return s.Groups.SetPrimaryImage(txCtx, in.GroupID, in.ImageID)
	})
	if err != nil {
		return dto.AdminVariantGroupDetailOutput{}, err
	}
	s.recordGroup(ctx, constant.AuditVariantGroupImagePrimarySet, in.GroupID,
		map[string]any{"imageId": in.ImageID.String()})
	return s.adminGroupDetail(ctx, in.GroupID)
}

// adminGroupDetail reads one group's administrator detail: the group, its own
// gallery and every option, including the ones a customer cannot see (FR-013).
func (s *Service) adminGroupDetail(ctx context.Context, id uuid.UUID) (dto.AdminVariantGroupDetailOutput, error) {
	view, err := s.Groups.FindByID(ctx, id)
	if err != nil {
		return dto.AdminVariantGroupDetailOutput{}, err
	}
	return s.Mapper.AdminVariantGroupDetail(*view), nil
}

// recordGroup emits one administrative audit event for a group write.
//
// The event always names the acting administrator when the call has a session, and
// the group identifier as the target. Metadata carries only the operator's own slug
// or the picture identifier, never a customer's personal data (FR-014, SC-006,
// Constitution VI).
//
// Emission never fails the business operation: the shared writer queues and reports
// a dropped event through the logger. An absent auditor is tolerated so a read-only
// construction of the service cannot panic on a write it never serves.
func (s *Service) recordGroup(ctx context.Context, action string, targetID uuid.UUID, metadata map[string]any) {
	if s.Audit == nil {
		return
	}
	s.Audit.Record(ctx, action, string(audit.OutcomeSuccess),
		auditActor(ctx), auditActorRole(ctx), targetTypeVariantGroup, targetID.String(), metadata)
}

// newGroupImage builds the picture a new upload becomes, before the provider has
// answered. The position and the primary flag are placeholders that insertGroupImage
// recomputes from the stored pictures; the identifier is fixed here so the audit
// entry and the response name the same picture.
func newGroupImage(groupID uuid.UUID, existing []model.VariantGroupImage) *model.VariantGroupImage {
	return &model.VariantGroupImage{
		ID:        uuid.New(),
		GroupID:   groupID,
		Position:  nextGroupImagePosition(existing),
		IsPrimary: len(existing) == 0,
		CreatedAt: now(),
	}
}

// nextGroupImagePosition reports the position a newly added group picture takes:
// after every existing one, so insertion order is preserved even after removals.
func nextGroupImagePosition(images []model.VariantGroupImage) int {
	next := 0
	for i, image := range images {
		if i == 0 || image.Position >= next {
			next = image.Position + 1
		}
	}
	return next
}

// groupImagePromotionAfterRemoval reports which remaining picture becomes the main
// one when removedID is removed, or false when nobody should. It mirrors the
// product gallery's rule: promotion happens only when the removed picture was the
// main one and pictures remain, and the promoted picture is the next by position
// then identifier.
func groupImagePromotionAfterRemoval(images []model.VariantGroupImage, removedID uuid.UUID) (uuid.UUID, bool) {
	var removed *model.VariantGroupImage
	for i := range images {
		if images[i].ID == removedID {
			removed = &images[i]
			break
		}
	}
	if removed == nil || !removed.IsPrimary {
		return uuid.Nil, false
	}

	remaining := make([]model.VariantGroupImage, 0, len(images))
	for _, image := range images {
		if image.ID == removedID {
			continue
		}
		remaining = append(remaining, image)
	}
	if len(remaining) == 0 {
		return uuid.Nil, false
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		if remaining[i].Position != remaining[j].Position {
			return remaining[i].Position < remaining[j].Position
		}
		return remaining[i].ID.String() < remaining[j].ID.String()
	})
	return remaining[0].ID, true
}
