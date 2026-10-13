import { spawn } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, rmSync } from "node:fs";
import { resolve } from "node:path";

export default async function setup() {
  const dir = resolve("../.local");
  mkdirSync(dir, { recursive: true });
  const urlFile = resolve(dir, "fixture-url.txt");
  if (existsSync(urlFile)) rmSync(urlFile);
  const child = spawn(
    "go",
    [
      "test",
      "./backend",
      "-run",
      "^TestBrowserFixture$",
      "-count=1",
      "-v",
      "-timeout",
      "6m",
    ],
    {
      cwd: resolve(".."),
      env: {
        ...process.env,
        TINYFAKA_BROWSER_FIXTURE: "1",
        TINYFAKA_FIXTURE_URL_FILE: urlFile,
      },
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: true,
    },
  );
  let output = "";
  child.stdout.on("data", (data) => {
    output += String(data);
  });
  child.stderr.on("data", (data) => {
    output += String(data);
  });
  let ready = false;
  for (let attempt = 0; attempt < 120; attempt++) {
    if (child.exitCode !== null) throw new Error(`后端启动失败：${output}`);
    if (existsSync(urlFile)) {
      process.env.TINYFAKA_TEST_URL = readFileSync(urlFile, "utf8");
      ready = true;
      break;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  if (!ready) {
    child.kill();
    throw new Error(`后端启动超时：${output}`);
  }
  return async () => {
    // go test 可能派生测试进程；Windows 原生 taskkill 仅终止本次创建的进程树。
    if (process.platform === "win32" && child.pid) {
      await new Promise<void>((done) => {
        const stop = spawn(
          "taskkill",
          ["/PID", String(child.pid), "/T", "/F"],
          { windowsHide: true, stdio: "ignore" },
        );
        stop.on("exit", () => done());
        stop.on("error", () => done());
      });
    } else {
      child.kill("SIGTERM");
    }
  };
}
