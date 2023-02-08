
export default function copyToClipboard(text, onSucc, onError) {
    if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard
            .writeText(text)
            .then(() => onSucc())
            .catch(() => onError())
    } else {
        // 创建text area
        const textArea = document.createElement('textarea')
        textArea.value = text
        // 使text area不在viewport，同时设置不可见
        document.body.appendChild(textArea)
        textArea.focus()
        textArea.select()
        return new Promise((resolve, reject) => {
            // 执行复制命令并移除文本框
            document.execCommand('copy') ? resolve() : reject(new Error('出错了'))
            textArea.remove()
        }).then(
            () => onSucc(),
            () => onError()
        )
    }
}