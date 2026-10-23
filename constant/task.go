package constant

type TaskPlatform string

const (
	TaskPlatformSuno       TaskPlatform = "suno"
	TaskPlatformMidjourney              = "mj"
	// CUSTOM: 豆包语音异步长文本合成（fork 扩展）。由路由显式指定，
	// 不随渠道类型推导，否则会和同为火山渠道的视频任务撞在一起。
	TaskPlatformDoubaoTTS TaskPlatform = "doubao-tts"
)

const (
	TaskActionSpeech = "speech"
)

const (
	SunoActionMusic  = "MUSIC"
	SunoActionLyrics = "LYRICS"

	TaskActionGenerate          = "generate"
	TaskActionTextGenerate      = "textGenerate"
	TaskActionFirstTailGenerate = "firstTailGenerate"
	TaskActionReferenceGenerate = "referenceGenerate"
	TaskActionRemix             = "remixGenerate"
)

var SunoModel2Action = map[string]string{
	"suno_music":  SunoActionMusic,
	"suno_lyrics": SunoActionLyrics,
}
