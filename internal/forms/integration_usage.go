package forms

import "gorm.io/gorm"

// MailerProfileUsage returns the number of form deliveries using a mailer profile.
func MailerProfileUsage(db *gorm.DB, id uint) (int64, error) {
	var count int64
	if err := db.Model(&EmailDelivery{}).Where("mailer_profile_id = ?", id).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CaptchaProfileUsage returns the number of forms using a captcha profile.
func CaptchaProfileUsage(db *gorm.DB, id uint) (int64, error) {
	var count int64
	if err := db.Model(&Form{}).Where("captcha_profile_id = ?", id).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
