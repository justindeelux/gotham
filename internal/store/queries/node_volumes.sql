-- name: ListApplicationIDs :many
-- Every recorded application owns the Docker volumes named after its id. A
-- deleted application row leaves its volumes behind, which is what the
-- orphaned-volume view lists.
SELECT id FROM applications;

-- name: ListServiceIDs :many
-- A soft-deleted service's volumes were removed by the delete, so a deleted
-- service is not an owner. A volume that survived a failed removal is listed.
SELECT id FROM services WHERE deleted_at IS NULL;

-- name: ListDatabaseVolumeNames :many
-- Soft-deleted databases keep their volume for the retention window, so they
-- still own it.
SELECT storage_path FROM databases WHERE storage_path <> '';
