package receive

import (
	"context"
	"fmt"

	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/object"
)

func (s *Server) getCGet(ctx context.Context, req dimse.CGetRequestContext) ([]dimse.CGetSubOperation, error) {
	instances, err := s.instancesForRetrieve(ctx, req.QueryRetrieveLevel, req.Identifier, "C-GET")
	if err != nil {
		return nil, err
	}
	ops := make([]dimse.CGetSubOperation, 0, len(instances))
	for _, inst := range instances {
		inst := inst
		ops = append(ops, dimse.CGetSubOperation{
			AffectedSOPClassUID:    inst.SOPClassUID,
			AffectedSOPInstanceUID: inst.SOPInstanceUID,
			LoadDataSet: func(context.Context) (*object.Object, error) {
				file, err := object.OpenFile(inst.StoredPath)
				if err != nil {
					return nil, err
				}
				defer file.Close()
				if file.Dataset == nil {
					return nil, fmt.Errorf("%s: DICOM file has no dataset", inst.StoredPath)
				}
				return file.Dataset, nil
			},
		})
	}
	return ops, nil
}
