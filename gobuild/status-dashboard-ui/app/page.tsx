"use client";

import { useEffect, useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

type BuildStatus = {
  id: string;
  repository_url: string;
  branch?: string;
  commit_hash?: string;
  status: string;
  message?: string;
  created_at: string;
  updated_at: string;
  artifact_url?: string;
  logs?: string[];
};

export default function Dashboard() {
  const [builds, setBuilds] = useState<BuildStatus[]>([]);
  const [selectedBuild, setSelectedBuild] = useState<BuildStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [repositoryUrl, setRepositoryUrl] = useState("");
  const [branch, setBranch] = useState("");
  const [socket, setSocket] = useState<WebSocket | null>(null);

  // Fetch builds on component mount
  useEffect(() => {
    const fetchBuilds = async () => {
      try {
        const response = await fetch("http://localhost:8085/api/builds");
        if (!response.ok) {
          throw new Error("Failed to fetch builds");
        }
        const data = await response.json();
        setBuilds(data);
      } catch (error) {
        console.error("Error fetching builds:", error);
      } finally {
        setLoading(false);
      }
    };

    fetchBuilds();
  }, []);

  // Set up WebSocket connection
  useEffect(() => {
    const ws = new WebSocket("ws://localhost:8084/ws?clientId=dashboard-ui&buildId=");

    ws.onopen = () => {
      console.log("WebSocket connection established");
      setSocket(ws);
    };

    ws.onmessage = (event) => {
      const data = JSON.parse(event.data);
      console.log("WebSocket message received:", data);

      // Update builds based on the message type
      if (data.type === "status") {
        setBuilds(prevBuilds => {
          const updatedBuilds = [...prevBuilds];
          const buildIndex = updatedBuilds.findIndex(build => build.id === data.buildId);

          if (buildIndex >= 0) {
            updatedBuilds[buildIndex] = {
              ...updatedBuilds[buildIndex],
              status: data.status,
              message: data.message,
              updated_at: data.time
            };
          }

          return updatedBuilds;
        });

        // Update selected build if it's the one being updated
        if (selectedBuild && selectedBuild.id === data.buildId) {
          setSelectedBuild(prevBuild => {
            if (!prevBuild) return null;
            return {
              ...prevBuild,
              status: data.status,
              message: data.message,
              updated_at: data.time
            };
          });
        }
      } else if (data.type === "log") {
        // Update logs for the selected build
        if (selectedBuild && selectedBuild.id === data.buildId) {
          setSelectedBuild(prevBuild => {
            if (!prevBuild) return null;
            return {
              ...prevBuild,
              logs: [...(prevBuild.logs || []), data.log]
            };
          });
        }
      } else if (data.type === "completion") {
        setBuilds(prevBuilds => {
          const updatedBuilds = [...prevBuilds];
          const buildIndex = updatedBuilds.findIndex(build => build.id === data.buildId);

          if (buildIndex >= 0) {
            updatedBuilds[buildIndex] = {
              ...updatedBuilds[buildIndex],
              status: data.status,
              artifact_url: data.artifactUrl,
              updated_at: data.time
            };
          }

          return updatedBuilds;
        });

        // Update selected build if it's the one being updated
        if (selectedBuild && selectedBuild.id === data.buildId) {
          setSelectedBuild(prevBuild => {
            if (!prevBuild) return null;
            return {
              ...prevBuild,
              status: data.status,
              artifact_url: data.artifactUrl,
              updated_at: data.time
            };
          });
        }
      }
    };

    ws.onerror = (error) => {
      console.error("WebSocket error:", error);
    };

    ws.onclose = () => {
      console.log("WebSocket connection closed");
      setSocket(null);
    };

    return () => {
      ws.close();
    };
  }, []);

  // Handle build selection
  const handleBuildSelect = async (build: BuildStatus) => {
    try {
      const response = await fetch(`http://localhost:8085/api/builds/${build.id}`);
      if (!response.ok) {
        throw new Error("Failed to fetch build details");
      }
      const data = await response.json();
      setSelectedBuild(data);
    } catch (error) {
      console.error("Error fetching build details:", error);
    }
  };

  // Handle new build submission
  const handleSubmitBuild = async () => {
    if (!repositoryUrl) return;

    try {
      const response = await fetch("http://localhost:8080/api/builds", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          // In a real app, you would include authentication
          "Authorization": "Bearer mocktokenfornow"
        },
        body: JSON.stringify({
          repository_url: repositoryUrl,
          branch: branch || undefined
        })
      });

      if (!response.ok) {
        throw new Error("Failed to submit build");
      }

      const data = await response.json();
      console.log("Build submitted:", data);

      // Reset form
      setRepositoryUrl("");
      setBranch("");

      // Refresh builds
      const buildsResponse = await fetch("http://localhost:8085/api/builds");
      if (buildsResponse.ok) {
        const buildsData = await buildsResponse.json();
        setBuilds(buildsData);
      }
    } catch (error) {
      console.error("Error submitting build:", error);
    }
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case "completed":
      case "success":
        return "bg-green-500";
      case "in-progress":
      case "queued":
        return "bg-blue-500";
      case "failed":
      case "failure":
        return "bg-red-500";
      default:
        return "bg-gray-500";
    }
  };

  return (
      <div className="container mx-auto py-10">
        <h1 className="text-3xl font-bold mb-8">GoBuild Status Dashboard</h1>

        <Tabs defaultValue="builds">
          <TabsList className="mb-4">
            <TabsTrigger value="builds">Builds</TabsTrigger>
            <TabsTrigger value="new">New Build</TabsTrigger>
          </TabsList>

          <TabsContent value="builds">
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
              <div className="lg:col-span-1">
                <h2 className="text-xl font-semibold mb-4">Recent Builds</h2>

                {loading ? (
                    <p>Loading builds...</p>
                ) : builds.length === 0 ? (
                    <Alert>
                      <AlertTitle>No builds found</AlertTitle>
                      <AlertDescription>
                        Submit a new build to get started.
                      </AlertDescription>
                    </Alert>
                ) : (
                    <div className="space-y-4">
                      {builds.map((build) => (
                          <Card
                              key={build.id}
                              className={`cursor-pointer ${selectedBuild?.id === build.id ? 'border-blue-500' : ''}`}
                              onClick={() => handleBuildSelect(build)}
                          >
                            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                              <CardTitle className="text-sm font-medium">
                                {build.repository_url.split('/').pop()}
                              </CardTitle>
                              <Badge className={getStatusColor(build.status)}>
                                {build.status}
                              </Badge>
                            </CardHeader>
                            <CardContent>
                              <p className="text-xs text-gray-500 truncate">{build.repository_url}</p>
                              <p className="text-xs text-gray-500 mt-1">
                                {new Date(build.created_at).toLocaleString()}
                              </p>
                            </CardContent>
                          </Card>
                      ))}
                    </div>
                )}
              </div>

              <div className="lg:col-span-2">
                <h2 className="text-xl font-semibold mb-4">Build Details</h2>

                {selectedBuild ? (
                    <Card>
                      <CardHeader>
                        <div className="flex justify-between items-center">
                          <CardTitle>{selectedBuild.repository_url.split('/').pop()}</CardTitle>
                          <Badge className={getStatusColor(selectedBuild.status)}>
                            {selectedBuild.status}
                          </Badge>
                        </div>
                        <CardDescription>
                          Repository: {selectedBuild.repository_url}<br />
                          {selectedBuild.branch && `Branch: ${selectedBuild.branch}`}<br />
                          Created: {new Date(selectedBuild.created_at).toLocaleString()}<br />
                          Updated: {new Date(selectedBuild.updated_at).toLocaleString()}
                        </CardDescription>
                      </CardHeader>
                      <CardContent>
                        <h3 className="font-semibold mb-2">Logs</h3>
                        <div className="bg-black text-white p-4 rounded font-mono text-xs h-64 overflow-y-auto">
                          {selectedBuild.logs && selectedBuild.logs.length > 0 ? (
                              selectedBuild.logs.map((log, index) => (
                                  <div key={index}>{log}</div>
                              ))
                          ) : (
                              <p>No logs available</p>
                          )}
                        </div>

                        {selectedBuild.artifact_url && (
                            <div className="mt-4">
                              <Button asChild>
                                <a
                                    href={`http://localhost:8083${selectedBuild.artifact_url}`}
                                    target="_blank"
                                    rel="noopener noreferrer"
                                >
                                  Download Artifact
                                </a>
                              </Button>
                            </div>
                        )}
                      </CardContent>
                    </Card>
                ) : (
                    <Alert>
                      <AlertTitle>No build selected</AlertTitle>
                      <AlertDescription>
                        Select a build from the list to view details.
                      </AlertDescription>
                    </Alert>
                )}
              </div>
            </div>
          </TabsContent>

          <TabsContent value="new">
            <Card>
              <CardHeader>
                <CardTitle>Submit New Build</CardTitle>
                <CardDescription>
                  Enter repository details to start a new build.
                </CardDescription>
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
                    <Input
                        id="branch"
                        placeholder="main"
                        value={branch}
                        onChange={(e) => setBranch(e.target.value)}
                    />
                  </div>
                  <Button onClick={handleSubmitBuild} disabled={!repositoryUrl}>
                    Submit Build
                  </Button>
                </div>
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>
      </div>
  );
}