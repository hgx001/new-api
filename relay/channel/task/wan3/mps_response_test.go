package wan3

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// DescribeTaskDetail 响应解析（2026-10-30 真机实测踩到的坑）
//
// 症状：MPS 侧任务 8 秒就完成了，站内任务却一直 in_progress，
//
//	日志刷「post process poll failed: unmarshal DescribeTaskDetail response failed:
//	json: cannot unmarshal number into Go struct field ...ErrCode of type string」
//
// 根因：腾讯云 DescribeTaskDetail 返回的 ErrCode 是**数字**（成功时 0），
//	代码声明成 string → 每次轮询解析失败 → 第二跳永远拿不到产物路径。
//
// 下面的样本是从生产环境**真实响应**里裁出来的（原样保留了字段形态）。
// ─────────────────────────────────────────────────────────────────────────────

// realDescribeTaskDetailResponse 是生产实测的真实响应（成功场景）。
const realDescribeTaskDetailResponse = `{
  "Response": {
    "TaskType": "WorkflowTask",
    "Status": "FINISH",
    "CreateTime": "2026-10-29T23:35:05Z",
    "BeginProcessTime": "2026-10-29T23:35:05Z",
    "FinishTime": "2026-10-29T23:35:13Z",
    "WorkflowTask": {
      "TaskId": "2600033414-WorkflowTask-f4427ff23febb8afd1f0e515f9218202tt7",
      "Status": "FINISH",
      "ErrCode": 0,
      "Message": "SUCCESS",
      "InputInfo": {
        "Type": "URL",
        "UrlInputInfo": {"Url": "https://example.com/in.mp4?sign=abc"}
      },
      "MetaData": {
        "Duration": 2.02,
        "Height": 480,
        "Width": 854,
        "VideoStreamSet": [{"Codec":"h264","Fps":30,"Height":480,"Width":854,"Duration":2}]
      },
      "MediaProcessResultSet": [
        {
          "Type": "Transcode",
          "TranscodeTask": {
            "Status": "SUCCESS",
            "ErrCode": 0,
            "ErrCodeExt": "",
            "Message": "SUCCESS",
            "BeginProcessTime": "2026-10-29T23:35:05Z",
            "FinishTime": "2026-10-29T23:35:12Z",
            "Input": {
              "Definition": 0,
              "DefinitionType": "Enhance",
              "RawParameter": {
                "Container": "mp4",
                "VideoTemplate": {
                  "Codec": "h264", "Fps": 30, "Bitrate": 5000,
                  "ResolutionAdaptive": "open", "Width": 1280, "Height": 720,
                  "Gop": 60, "FillType": "black"
                },
                "AudioTemplate": {"Codec":"aac","Bitrate":64,"SampleRate":44100,"AudioChannel":2},
                "EnhanceConfig": {
                  "VideoEnhance": {
                    "SuperResolution": {"Switch":"ON","Type":"hq","Size":2}
                  }
                }
              }
            },
            "Output": {
              "OutputStorage": {
                "Type": "COS",
                "CosOutputStorage": {"Bucket":"mps-test-1397292858","Region":"ap-guangzhou"}
              },
              "Path": "/mps_api_test/9211e95e53244b1daf7313b329023eaf-metadata_elJ3929n_ef947a0f869f_transcode_0.mp4",
              "Bitrate": 3340591,
              "Height": 720,
              "Width": 1280,
              "Size": 843917,
              "Duration": 2.021
            }
          }
        }
      ]
    }
  }
}`

// TestParseDescribeTaskDetailRealResponse 锁定真实响应能解析成功。
//
// 这条测试直接对应线上故障：解析失败 → 轮询永远拿不到产物 → 任务挂到超时。
func TestParseDescribeTaskDetailRealResponse(t *testing.T) {
	var resp mpsResponse
	require.NoError(t, json.Unmarshal([]byte(realDescribeTaskDetailResponse), &resp),
		"真实响应必须能解析（ErrCode 是数字形态）")

	require.Equal(t, "FINISH", resp.Response.Status)
	require.NotNil(t, resp.Response.WorkflowTask)
	require.Equal(t, "FINISH", resp.Response.WorkflowTask.Status)

	// 产物路径必须能取到 —— 取不到就等于没加工
	body := resp.Response.WorkflowTask
	require.Len(t, body.MediaProcessResultSet, 1)
	transcode := body.MediaProcessResultSet[0].TranscodeTask
	require.NotNil(t, transcode)
	require.Equal(t, "SUCCESS", transcode.Status)
	require.Equal(t, "/mps_api_test/9211e95e53244b1daf7313b329023eaf-metadata_elJ3929n_ef947a0f869f_transcode_0.mp4",
		transcode.outputPath())

	// 产物确实是 720P
	require.Equal(t, int64(1280), transcode.Output.Width)
	require.Equal(t, int64(720), transcode.Output.Height)
}

// TestMPSErrCodeAcceptsBothForms 锁定兼容两种形态。
func TestMPSErrCodeAcceptsBothForms(t *testing.T) {
	// 数字形态
	var numeric mpsErrCode
	require.NoError(t, json.Unmarshal([]byte("0"), &numeric))
	require.Equal(t, "0", numeric.String())

	// 字符串形态
	var text mpsErrCode
	require.NoError(t, json.Unmarshal([]byte(`"InvalidParameter"`), &text))
	require.Equal(t, "InvalidParameter", text.String())

	// null / 空
	var empty mpsErrCode
	require.NoError(t, json.Unmarshal([]byte("null"), &empty))
	require.Equal(t, "", empty.String())
}

// TestParseDescribeTaskDetailWithStringErrCode 锁定字符串形态也不炸。
func TestParseDescribeTaskDetailWithStringErrCode(t *testing.T) {
	payload := `{"Response":{"Status":"FAIL","ErrCode":"InternalError",
	  "WorkflowTask":{"Status":"FAIL","ErrCode":"InternalError","Message":"boom"}}}`
	var resp mpsResponse
	require.NoError(t, json.Unmarshal([]byte(payload), &resp))
	require.Equal(t, "FAIL", resp.Response.Status)
	require.Equal(t, "InternalError", resp.Response.ErrCode.String())
}
