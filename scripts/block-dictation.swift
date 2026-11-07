import Foundation
import AVFoundation
import Speech

final class Dictation {
    private let engine = AVAudioEngine()
    private let request = SFSpeechAudioBufferRecognitionRequest()
    private var task: SFSpeechRecognitionTask?
    private var installedTap = false
    private var finished = false

    func emit(_ type: String, _ text: String = "") {
        guard let data = try? JSONSerialization.data(withJSONObject: ["type": type, "text": text]),
              let line = String(data: data, encoding: .utf8) else { return }
        print(line)
        fflush(stdout)
    }

    func finish(_ error: String? = nil) {
        guard !finished else { return }
        finished = true
        if engine.isRunning { engine.stop() }
        if installedTap { engine.inputNode.removeTap(onBus: 0) }
        request.endAudio()
        task?.cancel()
        if let error = error { emit("error", error) } else { emit("done") }
        exit(error == nil ? 0 : 1)
    }

    func start() {
        SFSpeechRecognizer.requestAuthorization { status in
            DispatchQueue.main.async {
                guard status == .authorized else { self.finish("speech_permission"); return }
                AVCaptureDevice.requestAccess(for: .audio) { granted in
                    DispatchQueue.main.async {
                        guard granted else { self.finish("microphone_permission"); return }
                        self.beginRecognition()
                    }
                }
            }
        }
    }

    private func beginRecognition() {
        guard let recognizer = SFSpeechRecognizer(), recognizer.isAvailable else {
            finish("unavailable"); return
        }
        request.shouldReportPartialResults = true
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0 else { finish("audio_unavailable"); return }
        input.installTap(onBus: 0, bufferSize: 1024, format: format) { buffer, _ in
            self.request.append(buffer)
        }
        installedTap = true
        task = recognizer.recognitionTask(with: request) { result, error in
            DispatchQueue.main.async {
                if let result = result {
                    self.emit("text", result.bestTranscription.formattedString)
                    if result.isFinal { self.finish(); return }
                }
                if error != nil { self.finish("recognition_failed") }
            }
        }
        do {
            engine.prepare()
            try engine.start()
            emit("ready")
        } catch { finish("audio_unavailable") }
    }
}

let dictation = Dictation()
DispatchQueue.global().async {
    while readLine() != nil {}
    DispatchQueue.main.async { dictation.finish() }
}
dictation.start()
dispatchMain()
