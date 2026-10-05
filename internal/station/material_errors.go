package station

import "example.com/paleo-prep-station/internal/contracts"

func notFoundBatch(id string) error {
	return contracts.NotFound("batch_not_found", "未找到耗材批次 "+id)
}

func notFoundMaterialLine(planID, batchID string) error {
	return contracts.NotFound(
		"material_line_not_found",
		"方案 "+planID+" 尚未领用耗材批次 "+batchID,
	)
}
