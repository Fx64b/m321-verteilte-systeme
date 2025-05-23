"use client"
import { useRouter } from "next/navigation"
import { useEffect, useState } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"

type BuildStatus = {
  id: string
  repository_url: string
  branch?: string
  commit_hash?: string
  status: string
  message?: string
  created_at: string
  updated_at: string
  artifact_url?: string
  logs?: string[]
}

type User = {
  id: string
  email: string
  role: string
}

export default function Dashboard() {
  const [builds, setBuilds] = useState<BuildStatus[]>([])
  const [selectedBuild, setSelectedBuild] = useState<BuildStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [repositoryUrl, setRepositoryUrl] = useState("")
  const [branch, setBranch] = useState("")

  // Authentication state
  const [user, setUser] = useState<User | null>(null)
  const [showLoginForm, setShowLoginForm] = useState(false)
  const [loginEmail, setLoginEmail] = useState("")
  const [loginPassword, setLoginPassword] = useState("")
  const [registerEmail, setRegisterEmail] = useState("")
  const [registerPassword, setRegisterPassword] = useState("")
  const [isRegistering, setIsRegistering] = useState(false)
  const [authError, setAuthError] = useState("")

  const router = useRouter()

  // Check if user is already logged in
  useEffect(() => {
    const storedUser = localStorage.getItem("user")
    if (storedUser) {
      try {
        setUser(JSON.parse(storedUser))
      } catch (e) {
        console.error("Failed to parse stored user:", e)
        localStorage.removeItem("user")
      }
    }
  }, [])

  // Fetch builds on component mount
  useEffect(() => {
    fetchBuilds()
  }, [])

  const fetchBuilds = async () => {
    try {
      const token = localStorage.getItem("authToken")
      const response = await fetch("http://localhost:8086/api/builds", {
        headers: token
            ? {
              Authorization: `Bearer ${token}`,
            }
            : {},
      })
      if (!response.ok) {
        throw new Error("Failed to fetch builds")
      }
      const data = await response.json()
      setBuilds(data)
    } catch (error) {
      console.error("Error fetching builds:", error)
    } finally {
      setLoading(false)
    }
  }

  // Set up WebSocket connection
  useEffect(() => {
    const ws = new WebSocket("ws://localhost:8085/ws?clientId=dashboard-ui&buildId=")

    ws.onopen = () => {
      console.log("WebSocket connection established")
    }

    ws.onmessage = (event) => {
      const data = JSON.parse(event.data)
      console.log("WebSocket message received:", data)

      // Update builds based on the message type
      if (data.type === "status") {
        setBuilds((prevBuilds) => {
          const updatedBuilds = [...prevBuilds]
          const buildIndex = updatedBuilds.findIndex((build) => build.id === data.buildId)

          if (buildIndex >= 0) {
            updatedBuilds[buildIndex] = {
              ...updatedBuilds[buildIndex],
              status: data.status,
              message: data.message,
              updated_at: data.time,
            }
          }

          return updatedBuilds
        })

        // Update selected build if it's the one being updated
        if (selectedBuild && selectedBuild.id === data.buildId) {
          setSelectedBuild((prevBuild) => {
            if (!prevBuild) return null
            return {
              ...prevBuild,
              status: data.status,
              message: data.message,
              updated_at: data.time,
            }
          })
        }
      } else if (data.type === "log") {
        // Update logs for the selected build
        if (selectedBuild && selectedBuild.id === data.buildId) {
          setSelectedBuild((prevBuild) => {
            if (!prevBuild) return null
            return {
              ...prevBuild,
              logs: [...(prevBuild.logs || []), data.log],
            }
          })
        }
      } else if (data.type === "completion") {
        setBuilds((prevBuilds) => {
          const updatedBuilds = [...prevBuilds]
          const buildIndex = updatedBuilds.findIndex((build) => build.id === data.buildId)

          if (buildIndex >= 0) {
            updatedBuilds[buildIndex] = {
              ...updatedBuilds[buildIndex],
              status: data.status,
              artifact_url: data.artifactUrl,
              updated_at: data.time,
            }
          }

          return updatedBuilds
        })

        // Update selected build if it's the one being updated
        if (selectedBuild && selectedBuild.id === data.buildId) {
          setSelectedBuild((prevBuild) => {
            if (!prevBuild) return null
            return {
              ...prevBuild,
              status: data.status,
              artifact_url: data.artifactUrl,
              updated_at: data.time,
            }
          })
        }
      }
    }

    ws.onerror = (error) => {
      console.error("WebSocket error:", error)
    }

    ws.onclose = () => {
      console.log("WebSocket connection closed")
    }

    return () => {
      ws.close()
    }
  }, [])

  // Update the handleSubmitBuild function
  const handleSubmitBuild = async () => {
    if (!repositoryUrl) return

    try {
      const token = localStorage.getItem("authToken")
      if (!token) {
        setShowLoginForm(true)
        return
      }

      const response = await fetch("http://localhost:8081/api/builds", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          repository_url: repositoryUrl,
          branch: branch || undefined,
        }),
      })

      if (!response.ok) {
        throw new Error("Failed to submit build")
      }

      const data = await response.json()
      console.log("Build submitted:", data)

      // Navigate to the build details page
      router.push(`/builds/${data.build_id}`)
    } catch (error) {
      console.error("Error submitting build:", error)
    }
  }

  const getStatusColor = (status: string) => {
    switch (status) {
      case "completed":
      case "success":
        return "bg-green-500"
      case "in-progress":
      case "queued":
        return "bg-blue-500"
      case "failed":
      case "failure":
        return "bg-red-500"
      default:
        return "bg-gray-500"
    }
  }

  // Login function
  const handleLogin = async () => {
    try {
      setAuthError("")
      const response = await fetch("http://localhost:8081/api/login", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          email: loginEmail,
          password: loginPassword,
        }),
      })

      if (!response.ok) {
        const error = await response.text()
        throw new Error(error || "Login failed")
      }

      const data = await response.json()
      localStorage.setItem("authToken", data.token)
      localStorage.setItem("user", JSON.stringify(data.user))
      setUser(data.user)
      setShowLoginForm(false)
      setLoginEmail("")
      setLoginPassword("")

      // Refresh builds after login
      fetchBuilds()
    } catch (error: any) {
      console.error("Login error:", error)
      setAuthError(error.message)
    }
  }

  // Register function
  const handleRegister = async () => {
    try {
      setAuthError("")
      const response = await fetch("http://localhost:8081/api/register", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          email: registerEmail,
          password: registerPassword,
        }),
      })

      if (!response.ok) {
        const error = await response.text()
        throw new Error(error || "Registration failed")
      }

      const data = await response.json()
      localStorage.setItem("authToken", data.token)
      localStorage.setItem("user", JSON.stringify(data.user))
      setUser(data.user)
      setShowLoginForm(false)
      setRegisterEmail("")
      setRegisterPassword("")

      // Refresh builds after registration
      fetchBuilds()
    } catch (error: any) {
      console.error("Registration error:", error)
      setAuthError(error.message)
    }
  }

  // Logout function
  const handleLogout = () => {
    localStorage.removeItem("authToken")
    localStorage.removeItem("user")
    setUser(null)
  }

  // Format repository name
  const formatRepoName = (url: string) => {
    try {
      const parts = url.split("/")
      return parts[parts.length - 2] + "/" + parts[parts.length - 1]
    } catch (e) {
        console.error("Error formatting repository name:", e)
      return url.split("/").pop() || url
    }
  }

  return (
      <div className="container mx-auto px-4 py-6">
        {/* Show login/register form if not authenticated */}
        {showLoginForm && !user && (
            <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
              <Card className="w-full max-w-md mx-auto">
                <CardHeader>
                  <CardTitle>{isRegistering ? "Create an Account" : "Sign In"}</CardTitle>
                  <CardDescription>
                    {isRegistering ? "Register to create and track builds" : "Sign in to your account to continue"}
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  {authError && (
                      <Alert variant="destructive" className="mb-4">
                        <AlertDescription>{authError}</AlertDescription>
                      </Alert>
                  )}

                  {isRegistering ? (
                      <div className="space-y-4">
                        <div className="space-y-2">
                          <Label htmlFor="register-email">Email</Label>
                          <Input
                              id="register-email"
                              type="email"
                              value={registerEmail}
                              onChange={(e) => setRegisterEmail(e.target.value)}
                          />
                        </div>
                        <div className="space-y-2">
                          <Label htmlFor="register-password">Password</Label>
                          <Input
                              id="register-password"
                              type="password"
                              value={registerPassword}
                              onChange={(e) => setRegisterPassword(e.target.value)}
                          />
                        </div>
                        <div className="flex justify-between">
                          <Button onClick={handleRegister} disabled={!registerEmail || !registerPassword}>
                            Register
                          </Button>
                          <Button variant="outline" onClick={() => setIsRegistering(false)}>
                            Already have an account?
                          </Button>
                        </div>
                      </div>
                  ) : (
                      <div className="space-y-4">
                        <div className="space-y-2">
                          <Label htmlFor="login-email">Email</Label>
                          <Input
                              id="login-email"
                              type="email"
                              value={loginEmail}
                              onChange={(e) => setLoginEmail(e.target.value)}
                          />
                        </div>
                        <div className="space-y-2">
                          <Label htmlFor="login-password">Password</Label>
                          <Input
                              id="login-password"
                              type="password"
                              value={loginPassword}
                              onChange={(e) => setLoginPassword(e.target.value)}
                          />
                        </div>
                        <div className="flex justify-between">
                          <Button onClick={handleLogin} disabled={!loginEmail || !loginPassword}>
                            Login
                          </Button>
                          <Button variant="outline" onClick={() => setIsRegistering(true)}>
                            Need an account?
                          </Button>
                        </div>
                        <Button variant="ghost" className="w-full" onClick={() => setShowLoginForm(false)}>
                          Cancel
                        </Button>
                      </div>
                  )}
                </CardContent>
              </Card>
            </div>
        )}

        {/* Header with title and user info */}
        <header className="flex justify-between items-center mb-8 pb-4 border-b">
          <h1 className="text-2xl font-bold">GoBuild Status Dashboard</h1>

          {/* User profile badge or login button */}
          <div>
            {user ? (
                <div className="flex items-center gap-3">
                  <div className="bg-primary text-white rounded-full w-8 h-8 flex items-center justify-center">
                    {user.email.charAt(0).toUpperCase()}
                  </div>
                  <div className="text-sm">
                    <div className="font-medium">{user.email}</div>
                    <Button variant="link" className="p-0 h-auto" onClick={handleLogout}>
                      Sign out
                    </Button>
                  </div>
                </div>
            ) : (
                <Button onClick={() => setShowLoginForm(true)}>Sign In</Button>
            )}
          </div>
        </header>

        <Tabs defaultValue="builds" className="space-y-6">
          <TabsList className="w-full max-w-md grid grid-cols-2">
            <TabsTrigger value="builds">Builds</TabsTrigger>
            <TabsTrigger value="new">New Build</TabsTrigger>
          </TabsList>

          <TabsContent value="builds">
            <div className="mb-6">
              <div className="flex justify-between items-center mb-4">
                <h2 className="text-xl font-semibold">Recent Builds</h2>
                <Button variant="outline" size="sm" onClick={fetchBuilds}>
                  Refresh
                </Button>
              </div>

              {loading ? (
                  <div className="flex justify-center py-8">
                    <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-primary"></div>
                  </div>
              ) : builds.length === 0 ? (
                  <Alert>
                    <AlertTitle>No builds found</AlertTitle>
                    <AlertDescription>Submit a new build to get started.</AlertDescription>
                  </Alert>
              ) : (
                  <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                    {builds.map((build) => (
                        <Card
                            key={build.id}
                            className="cursor-pointer hover:shadow-md transition-shadow"
                            onClick={() => router.push(`/builds/${build.id}`)}
                        >
                          <div className={`h-1 ${getStatusColor(build.status)}`} />
                          <CardHeader className="pb-2">
                            <div className="flex justify-between items-start">
                              <CardTitle className="text-base font-medium">{formatRepoName(build.repository_url)}</CardTitle>
                              <Badge className={getStatusColor(build.status)}>{build.status}</Badge>
                            </div>
                            <CardDescription className="truncate mt-1">{build.repository_url}</CardDescription>
                          </CardHeader>
                          <CardContent className="pt-0">
                            <div className="grid grid-cols-2 gap-2 text-sm">
                              {build.branch && (
                                  <div className="text-gray-600">
                                    <span className="font-medium">Branch:</span> {build.branch}
                                  </div>
                              )}
                              {build.commit_hash && (
                                  <div className="text-gray-600">
                                    <span className="font-medium">Commit:</span> {build.commit_hash.substring(0, 7)}
                                  </div>
                              )}
                              <div className="text-gray-500 text-xs col-span-2 mt-2">
                                {new Date(build.created_at).toLocaleString()}
                              </div>
                            </div>
                          </CardContent>
                        </Card>
                    ))}
                  </div>
              )}
            </div>
          </TabsContent>

          <TabsContent value="new">
            {!user ? (
                <Alert>
                  <AlertTitle>Authentication Required</AlertTitle>
                  <AlertDescription className="flex items-center justify-between">
                    <span>Please sign in to submit a new build.</span>
                    <Button className="ml-4" onClick={() => setShowLoginForm(true)}>
                      Sign In
                    </Button>
                  </AlertDescription>
                </Alert>
            ) : (
                <Card>
                  <CardHeader>
                    <CardTitle>Submit New Build</CardTitle>
                    <CardDescription>Enter repository details to start a new build.</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <div className="space-y-4">
                      <div className="space-y-2">
                        <Label htmlFor="repository">Repository URL</Label>
                        <Input
                            id="repository"
                            placeholder="https://github.com/username/repo"
                            value={repositoryUrl}
                            onChange={(e) => setRepositoryUrl(e.target.value)}
                        />
                      </div>
                      <div className="space-y-2">
                        <Label htmlFor="branch">Branch (optional)</Label>
                        <Input id="branch" placeholder="main" value={branch} onChange={(e) => setBranch(e.target.value)} />
                      </div>
                      <Button onClick={handleSubmitBuild} disabled={!repositoryUrl} className="w-full mt-2">
                        Submit Build
                      </Button>
                    </div>
                  </CardContent>
                </Card>
            )}
          </TabsContent>
        </Tabs>
      </div>
  )
}
