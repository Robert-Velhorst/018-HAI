package automation

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"automation-hub-backend/internal/config"
	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Handler struct {
	service Service
}

const automationMultipartOverhead int64 = 1 << 20

func NewHandler(service Service) *Handler {
	return &Handler{
		service: service,
	}
}

func DefaultHandler() *Handler {
	return NewHandler(DefaultService())
}

// RequireAuthenticatedOperator protects the shared local automation registry.
// Automation configuration is not per-owner data, but it can expose launch
// targets and trigger controlled runtimes. Browser/API callers must therefore
// carry a verified IDP identity; in-process schedulers continue to use service
// methods directly.
func RequireAuthenticatedOperator() gin.HandlerFunc {
	return func(c *gin.Context) {
		if verifiedAutomationActor(c) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "an authenticated operator session is required for automation access"})
			return
		}
		c.Next()
	}
}

func verifiedAutomationActor(c *gin.Context) string {
	if value, ok := c.Get(identity.ContextSubjectKey); ok {
		if subject, ok := value.(string); ok {
			return strings.TrimSpace(subject)
		}
	}
	return ""
}

func (h *Handler) ImageHandler(c *gin.Context) {
	imageName := c.Param("imageName")
	imagePath, err := resolveImagePath(imageName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := os.Stat(imagePath); err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Image not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.File(imagePath)
}

// Create
// @Summary Create a new automation
// @Description Create a new automation with the input data
// @Tags Automations
// @Accept  multipart/form-data
// @Produce  json
// @Param name formData string true "Automation Name"
// @Param host formData string true "Automation Host"
// @Param port formData int true "Automation Port"
// @Param position formData int true "Automation Position"
// @Param removeImage formData bool true "Remove Image"
// @Param id formData string false "Automation ID"
// @Param imageFile formData file false "Image File"
// @Success 201 {object} models.Automation "Successfully created automation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations [post]
func (h *Handler) Create(c *gin.Context) {
	var automation models.Automation
	maxBodyBytes := config.AppConfig.ImageMaxSize + automationMultipartOverhead
	if c.Request.ContentLength > maxBodyBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "automation upload exceeds the allowed size"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	if err := c.Request.ParseMultipartForm(automationMultipartOverhead); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "automation upload exceeds the allowed size"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid automation form"})
		return
	}

	automation.Name = c.PostForm("name")
	automation.Host = c.PostForm("host")
	port, _ := strconv.Atoi(c.PostForm("port"))
	automation.Port = port
	automation.LaunchType = c.PostForm("launchType")
	automation.LaunchTarget = c.PostForm("launchTarget")
	automation.RuntimeType = c.PostForm("runtimeType")
	automation.ServiceName = c.PostForm("serviceName")
	automation.RoutePath = c.PostForm("routePath")
	automation.PublicURL = c.PostForm("publicUrl")
	automation.LocalURL = c.PostForm("localUrl")
	automation.DependencyNotes = c.PostForm("dependencyNotes")
	automation.HealthCheckType = c.PostForm("healthCheckType")
	automation.HealthCheckURL = c.PostForm("healthCheckUrl")
	healthInterval, _ := strconv.Atoi(c.PostForm("healthCheckIntervalSeconds"))
	automation.HealthCheckIntervalSeconds = healthInterval
	expectedStatus, _ := strconv.Atoi(c.PostForm("expectedHttpStatus"))
	automation.ExpectedHTTPStatus = expectedStatus
	removeImage, _ := strconv.ParseBool(c.PostForm("removeImage"))
	automation.RemoveImage = removeImage

	file, _ := c.FormFile("imageFile")
	if file != nil {
		automation.ImageFile = file
	}

	newAutomation, err := h.service.Create(&automation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, newAutomation)
}

// GetAll
// @Summary Get all automations
// @Description Retrieve all automations
// @Tags Automations
// @Accept  json
// @Produce  json
// @Success 200 {array} models.Automation "Successfully retrieved automations"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations [get]
func (h *Handler) GetAll(c *gin.Context) {
	automations, err := h.service.FindAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, automations)
}

// GetByID
// @Summary Get an automation by ID
// @Description Retrieve a specific automation by its ID
// @Tags Automations
// @Accept  json
// @Produce  json
// @Param id path string true "Automation ID"
// @Success 200 {object} models.Automation "Successfully retrieved automation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations/{id} [get]
func (h *Handler) GetByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	automation, err := h.service.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Automation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if automation == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Automation not found"})
		return
	}

	c.JSON(http.StatusOK, automation)
}

// DeleteByID
// @Summary Delete an automation by ID
// @Description Delete a specific automation by its ID
// @Tags Automations
// @Accept  json
// @Produce  json
// @Param id path string true "Automation ID"
// @Success 204 "Successfully deleted automation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations/{id} [delete]
func (h *Handler) DeleteByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	err = h.service.Delete(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// SwapPosition
// @Summary Swap positions of two automations
// @Description Swap the positions of two specific automations by their IDs
// @Tags Automations
// @Accept  json
// @Produce  json
// @Param id1 path string true "First Automation ID"
// @Param id2 path string true "Second Automation ID"
// @Success 200 "Successfully swapped positions"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations/{id1}/swap/{id2} [patch]
func (h *Handler) SwapPosition(c *gin.Context) {
	id1Str := c.Param("id1")
	id2Str := c.Param("id2")

	id1, err := uuid.Parse(id1Str)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format for id1"})
		return
	}

	id2, err := uuid.Parse(id2Str)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format for id2"})
		return
	}

	err = h.service.SwapOrder(id1, id2)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

// Update
// @Summary Update an automation
// @Description Update a specific automation with the input data
// @Tags Automations
// @Accept  json
// @Produce  json
// @Param automation body models.Automation true "Automation data"
// @Success 200 {object} models.Automation "Successfully updated automation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
// @Router /automations [patch]
func (h *Handler) Update(c *gin.Context) {
	var automation models.Automation

	body, err := io.ReadAll(c.Request.Body)
	defer c.Request.Body.Close()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	if err := models.JSON.Unmarshal(body, &automation); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updatedAutomation, err := h.service.Update(&automation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, updatedAutomation)
}
