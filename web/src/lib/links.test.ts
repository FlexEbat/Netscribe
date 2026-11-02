import { describe, expect, it } from "vitest"
import { isIpAddress, isPort, sshCommand, webUrl } from "@/lib/links"

describe("webUrl", () => {
  it("opens the web ports and the services named http or https", () => {
    expect(webUrl("192.168.1.5", 80)).toBe("http://192.168.1.5")
    expect(webUrl("192.168.1.5", 443)).toBe("https://192.168.1.5")
    expect(webUrl("192.168.1.5", 8443)).toBe("https://192.168.1.5:8443")
    expect(webUrl("192.168.1.5", 8080)).toBe("http://192.168.1.5:8080")
    expect(webUrl("192.168.1.5", 8000)).toBe("http://192.168.1.5:8000")
    expect(webUrl("192.168.1.5", 8099)).toBe("http://192.168.1.5:8099")
    expect(webUrl("192.168.1.5", 9000)).toBe("http://192.168.1.5:9000")
    expect(webUrl("192.168.1.5", 3000, "http")).toBe("http://192.168.1.5:3000")
    expect(webUrl("192.168.1.5", 3000, "HTTPS")).toBe("https://192.168.1.5:3000")
  })

  it("writes an IPv6 address in brackets", () => {
    expect(webUrl("fd00::5", 8080)).toBe("http://[fd00::5]:8080")
    expect(webUrl("fd00::5", 443)).toBe("https://[fd00::5]")
  })

  it("gives nothing for a service that is not a web page", () => {
    expect(webUrl("192.168.1.5", 22, "ssh")).toBeNull()
    expect(webUrl("192.168.1.5", 3306, "mysql")).toBeNull()
    expect(webUrl("192.168.1.5", 8100)).toBeNull()
  })

  it("is not changed by a name that holds a script or a quote", () => {
    const clean = webUrl("192.168.1.5", 8080)
    expect(webUrl("192.168.1.5", 8080, 'javascript:alert(1)"')).toBe(clean)
    expect(webUrl("192.168.1.5", 8080, `x" onclick="steal()`)).toBe(clean)
    expect(webUrl("192.168.1.5", 3000, "javascript:alert(1)")).toBeNull()
  })

  it("accepts only a valid address and a port from 1 to 65535", () => {
    for (const address of [
      "javascript:alert(1)",
      '192.168.1.5"',
      "192.168.1.256",
      "192.168.1",
      "printer.local",
      "192.168.1.5/evil",
      "fd00::1%eth0",
      "[fd00::1]",
      "",
    ]) {
      expect(webUrl(address, 80), address).toBeNull()
      expect(sshCommand(address, 22), address).toBeNull()
    }
    for (const port of [0, -1, 65536, 80.5, Number.NaN, Number.POSITIVE_INFINITY]) {
      expect(webUrl("192.168.1.5", port, "http"), String(port)).toBeNull()
      expect(sshCommand("192.168.1.5", port, "ssh"), String(port)).toBeNull()
    }
    expect(webUrl("192.168.1.5", 65535, "http")).toBe("http://192.168.1.5:65535")
    expect(webUrl("192.168.1.5", 1, "http")).toBe("http://192.168.1.5:1")
  })
})

describe("sshCommand", () => {
  it("makes the command for port 22 and for a service named ssh", () => {
    expect(sshCommand("192.168.1.5", 22)).toBe("ssh 192.168.1.5")
    expect(sshCommand("fd00::5", 22, "ssh")).toBe("ssh fd00::5")
  })

  it("adds -p for another port", () => {
    expect(sshCommand("192.168.1.5", 2222, "ssh")).toBe("ssh 192.168.1.5 -p 2222")
  })

  it("gives nothing for a service that is not SSH", () => {
    expect(sshCommand("192.168.1.5", 80, "http")).toBeNull()
    expect(sshCommand("192.168.1.5", 2222)).toBeNull()
  })

  it("is not changed by a name that holds a quote or a command", () => {
    expect(sshCommand("192.168.1.5", 2222, "ssh; rm -rf /")).toBeNull()
    expect(sshCommand("192.168.1.5", 22, "ssh; rm -rf /")).toBe("ssh 192.168.1.5")
  })
})

describe("isIpAddress and isPort", () => {
  it("tell valid values from the rest", () => {
    expect(isIpAddress("10.0.0.1")).toBe(true)
    expect(isIpAddress("2001:db8::1")).toBe(true)
    expect(isIpAddress("::ffff:10.0.0.1")).toBe(true)
    expect(isIpAddress("10.0.0.1:80")).toBe(false)
    expect(isPort(22)).toBe(true)
    expect(isPort(0)).toBe(false)
  })
})
