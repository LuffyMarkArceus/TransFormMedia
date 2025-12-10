package image

type Repository interface {
	SaveImage(img *Image) error
	GetImageByID(id string) (*Image, error)
	ListImages(userID string, limit, offset int) ([]Image, error)
}
