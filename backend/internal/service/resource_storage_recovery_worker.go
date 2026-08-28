package service

import (
	"log"
	"math"
	"time"
)

const resourceStorageRecoveryLease = 2 * time.Minute

func (s *Service) startResourceStorageRecoveryWorker() {
	go func() {
		s.drainResourceStorageRecovery(32)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.drainResourceStorageRecovery(32)
		}
	}()
}

func (s *Service) drainResourceStorageRecovery(limit int) {
	owner := s.workerID
	if owner == "" {
		owner = newID()
	}
	for index := 0; index < limit; index++ {
		resource, err := s.repo.ClaimNextResourceCloudRecovery(owner, resourceStorageRecoveryLease)
		if err != nil {
			log.Printf("resource storage recovery claim failed: %v", err)
			return
		}
		if resource == nil {
			return
		}
		setting, err := s.ossSettingForResource(resource.UserID, resource)
		if err == nil {
			etag, putErr := s.putOSSObjectWithRetry(setting, resource.ObjectKey, resource.MimeType, resource.Size, resource.LocalBackupKey)
			if putErr == nil {
				if completeErr := s.repo.CompleteResourceCloudRecovery(resource.ID, owner, etag); completeErr != nil {
					log.Printf("resource storage recovery completion failed for %s: %v", resource.ID, completeErr)
				}
				continue
			}
			err = putErr
		}
		delay := resourceStorageRecoveryRetryDelay(resource.CloudSyncAttempts)
		if retryErr := s.repo.RetryResourceCloudRecovery(resource.ID, owner, storageErrorText(err), time.Now().Add(delay)); retryErr != nil {
			log.Printf("resource storage recovery retry update failed for %s: %v", resource.ID, retryErr)
		}
	}
}

func resourceStorageRecoveryRetryDelay(attempts int) time.Duration {
	exponent := math.Min(float64(max(attempts-1, 0)), 8)
	return time.Duration(math.Pow(2, exponent)) * resourceRecoveryInitialDelay
}
