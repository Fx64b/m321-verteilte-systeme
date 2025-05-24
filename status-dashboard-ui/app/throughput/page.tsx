"use client";

import { useEffect, useState, useRef } from "react";
import { useRouter } from "next/navigation";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Separator } from "@/components/ui/separator";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Progress } from "@/components/ui/progress";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  ArrowLeft,
  Plus,
  Trash2,
  Play,
  X,
  FileText,
  Zap,
  CheckCircle,
  XCircle,
  Clock,
  Activity,
} from "lucide-react";

interface Repository {
  id: string;
  url: string;
  name: string;
}

interface BuildStatus {
  buildId: string;
  repositoryId: string;
  status: "queued" | "in-progress" | "success" | "failed" | "completed";
  currentLog: string;
  startTime?: number;
  endTime?: number;
  duration?: number;
}

interface Metrics {
  totalBuilds: number;
  runningBuilds: number;
  completedBuilds: number;
  successfulBuilds: number;
  failedBuilds: number;
  queuedBuilds: number;
  averageDuration: number;
  minDuration: number;
  maxDuration: number;
  throughput: number;
  testStartTime?: number;
  testEndTime?: number;
  totalTestDuration?: number;
}

export default function ThroughputTestPage() {
  const router = useRouter();
  const [repositories, setRepositories] = useState<Repository[]>([]);
  const [newRepoUrl, setNewRepoUrl] = useState("");
  const [bulkRepos, setBulkRepos] = useState("");
  const [buildStatuses, setBuildStatuses] = useState<
    Record<string, BuildStatus>
  >({});
  const [isRunning, setIsRunning] = useState(false);
  const [metrics, setMetrics] = useState<Metrics>({
    totalBuilds: 0,
    runningBuilds: 0,
    completedBuilds: 0,
    successfulBuilds: 0,
    queuedBuilds: 0,
    failedBuilds: 0,
    averageDuration: 0,
    minDuration: 0,
    maxDuration: 0,
    throughput: 0,
  });
  const [socket, setSocket] = useState<WebSocket | null>(null);
  const [showBulkDialog, setShowBulkDialog] = useState(false);
  const [testComplete, setTestComplete] = useState(false);
  const metricsIntervalRef = useRef<NodeJS.Timeout | undefined>(undefined);

  // Check authentication
  useEffect(() => {
    const token = localStorage.getItem("authToken");
    if (!token) {
      router.push("/");
    }
  }, [router]);

  // Calculate metrics
  const calculateMetrics = () => {
    const statuses = Object.values(buildStatuses);
    const completed = statuses.filter(
      (s) =>
        s.status === "success" ||
        s.status === "failed" ||
        s.status === "completed",
    );
    const successful = statuses.filter(
      (s) => s.status === "success" || s.status === "completed",
    );
    const failed = statuses.filter((s) => s.status === "failed");
    const running = statuses.filter((s) => s.status === "in-progress");

    const durations = statuses
      .filter((s) => s.duration)
      .map((s) => s.duration!);

    const avgDuration =
      durations.length > 0
        ? durations.reduce((a, b) => a + b, 0) / durations.length
        : 0;

    const minDuration = durations.length > 0 ? Math.min(...durations) : 0;
    const maxDuration = durations.length > 0 ? Math.max(...durations) : 0;

    // Calculate throughput (builds per minute)
    let throughput = 0;
    if (metrics.testStartTime && completed.length > 0) {
      const elapsedMinutes = (Date.now() - metrics.testStartTime) / 60000;
      throughput = completed.length / elapsedMinutes;
    }

    setMetrics((prev) => ({
      ...prev,
      runningBuilds: running.length,
      completedBuilds: completed.length,
      successfulBuilds: successful.length,
      failedBuilds: failed.length,
      averageDuration: avgDuration,
      minDuration,
      maxDuration,
      throughput,
    }));

    // Check if test is complete
    if (
      statuses.length > 0 &&
      statuses.every(
        (s) =>
          s.status === "success" ||
          s.status === "failed" ||
          s.status === "completed",
      )
    ) {
      setTestComplete(true);
      setIsRunning(false);
      if (metricsIntervalRef.current) {
        clearInterval(metricsIntervalRef.current);
      }
      setMetrics((prev) => ({
        ...prev,
        testEndTime: Date.now(),
        totalTestDuration: prev.testStartTime
          ? Date.now() - prev.testStartTime
          : 0,
      }));
    }
  };

  // Update metrics when build statuses change
  useEffect(() => {
    calculateMetrics();
  }, [buildStatuses]);

  // Add repository
  const addRepository = (url: string) => {
    const trimmedUrl = url.trim();
    if (!trimmedUrl) return;

    const id = `repo-${Date.now()}-${Math.random()}`;
    const name = trimmedUrl.split("/").pop() || trimmedUrl;

    setRepositories((prev) => [...prev, { id, url: trimmedUrl, name }]);
    setNewRepoUrl("");
  };

  // Add bulk repositories
  const addBulkRepositories = () => {
    const urls = bulkRepos.split("\n").filter((url) => url.trim());
    urls.forEach((url) => addRepository(url));
    setBulkRepos("");
    setShowBulkDialog(false);
  };

  // Remove repository
  const removeRepository = (id: string) => {
    setRepositories((prev) => prev.filter((repo) => repo.id !== id));
  };

  const fetchBuildStatuses = async () => {
    try {
      const response = await fetch("http://localhost:8086/api/builds");
      if (!response.ok) {
        throw new Error("Failed to fetch builds");
      }
      return await response.json();
    } catch (error) {
      console.error("Error fetching builds:", error);
      return [];
    }
  };

  // Add a new state to track repository to build ID mapping
  const [repoBuildMap, setRepoBuildMap] = useState<Record<string, string>>({});

  // Add polling interval ref
  const pollingIntervalRef = useRef<NodeJS.Timeout | undefined>(undefined);

  // Modify startTest function to remove the old metrics interval
  const startTest = async () => {
    if (repositories.length === 0) return;

    setIsRunning(true);
    setTestComplete(false);
    setBuildStatuses({});
    setRepoBuildMap({});

    const testStartTime = Date.now();
    setMetrics({
      totalBuilds: repositories.length,
      runningBuilds: 0,
      completedBuilds: 0,
      successfulBuilds: 0,
      failedBuilds: 0,
      queuedBuilds: 0,
      averageDuration: 0,
      minDuration: 0,
      maxDuration: 0,
      throughput: 0,
      testStartTime,
    });

    // Remove the metrics interval - we'll use updateBuildStatuses exclusively
    // metricsIntervalRef.current = setInterval(calculateMetrics, 1000);

    const token = localStorage.getItem("authToken");
    const newRepoBuildMap: Record<string, string> = {};

    // Submit all builds concurrently
    const buildPromises = repositories.map(async (repo) => {
      try {
        const response = await fetch("http://localhost:8081/api/builds", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${token}`,
          },
          body: JSON.stringify({
            repository_url: repo.url,
          }),
        });

        if (!response.ok) {
          throw new Error("Failed to submit build");
        }

        const data = await response.json();

        // Store mapping of repo ID to build ID
        newRepoBuildMap[repo.id] = data.build_id;

        setBuildStatuses((prev) => ({
          ...prev,
          [data.build_id]: {
            buildId: data.build_id,
            repositoryId: repo.id,
            status: "queued",
            currentLog: "Build queued...",
          },
        }));

        return data.build_id;
      } catch (error) {
        console.error(`Failed to submit build for ${repo.url}:`, error);
        return null;
      }
    });

    await Promise.all(buildPromises);
    setRepoBuildMap(newRepoBuildMap);

    // Start polling for build statuses
    pollingIntervalRef.current = setInterval(updateBuildStatuses, 1000);
  };

  const updateBuildStatuses = async () => {
    const builds = await fetchBuildStatuses();

    // Create a mapping of repository URLs in our test
    const repoUrlMap: any = {};
    repositories.forEach((repo: any) => {
      repoUrlMap[repo.url] = repo.id;
    });

    const updatedBuildStatuses = { ...buildStatuses };
    const updatedRepoBuildMap = { ...repoBuildMap };

    // Count metrics only for builds that belong to our repositories
    let runningBuilds = 0;
    let completedBuilds = 0;
    let successfulBuilds = 0;
    let failedBuilds = 0;
    let queuedBuilds = 0;
    const durations: number[] = [];

    // First filter builds belonging to our repos
    const ourBuilds = builds.filter(
        (build: any) => repoUrlMap[build.repository_url],
    );

    ourBuilds.forEach((build: any) => {
      // Update metrics counts from API data
      if (build.status === "in-progress") {
        runningBuilds++;
      } else if (build.status === "success" || build.status === "completed") {
        completedBuilds++;
        successfulBuilds++;
        if (build.duration) durations.push(build.duration);
      } else if (build.status === "failed") {
        completedBuilds++;
        failedBuilds++;
        if (build.duration) durations.push(build.duration);
      } else if (build.status === "queued") {
        queuedBuilds++;
      }

      // Get repository ID for this build
      const repositoryId = repoUrlMap[build.repository_url];

      // Update our mapping with this build ID
      updatedRepoBuildMap[repositoryId] = build.id;

      // Get current status and keep existing log if available
      const currentStatus = updatedBuildStatuses[build.id] || {
        buildId: build.id,
        repositoryId,
        status: "queued",
      };

      // Map API status to our status enum
      let status = build.status;
      if (build.status === "completed") {
        status = "success";
      }

      // Create appropriate status message based on build status
      let statusMessage = currentStatus.currentLog;
      if (build.status === "success" || build.status === "completed") {
        statusMessage = build.message || "Build completed successfully";
      } else if (build.status === "failed") {
        statusMessage = build.message || "Build failed";
      } else if (build.status === "queued") {
        statusMessage = "Build queued for processing";
      }

      // Update with API data but preserve logs from WebSocket if they exist
      updatedBuildStatuses[build.id] = {
        ...currentStatus,
        status,
        currentLog: statusMessage,
        startTime: build.started_at
            ? new Date(build.started_at).getTime()
            : undefined,
        endTime: build.completed_at
            ? new Date(build.completed_at).getTime()
            : undefined,
        duration: build.duration,
      };
    });

    // Rest of the function remains the same
    const avgDuration = durations.length > 0
        ? durations.reduce((a, b) => a + b, 0) / durations.length
        : 0;

    const minDuration = durations.length > 0 ? Math.min(...durations) : 0;
    const maxDuration = durations.length > 0 ? Math.max(...durations) : 0;

    setBuildStatuses(updatedBuildStatuses);
    setRepoBuildMap(updatedRepoBuildMap);

    setMetrics((prev) => ({
      ...prev,
      runningBuilds,
      completedBuilds,
      successfulBuilds,
      failedBuilds,
      queuedBuilds,
      averageDuration: avgDuration,
      minDuration,
      maxDuration,
    }));
  };

  // Modify WebSocket message handler to only update logs
  useEffect(() => {
    if (!isRunning) return;

    const clientId = `throughput-test-${Date.now()}`;
    const ws = new WebSocket(
      `ws://localhost:8085/ws?clientId=${clientId}&buildId=`,
    );

    ws.onopen = () => {
      console.log("WebSocket connected for throughput test");
      setSocket(ws);
      console.debug(socket?.url);
    };

    ws.onmessage = (event) => {
      const data = JSON.parse(event.data);

      setBuildStatuses((prev) => {
        const updated = { ...prev };

        // Only update logs from WebSocket
        if ((data.type === "status" || data.type === "log") && data.buildId) {
          if (updated[data.buildId]) {
            updated[data.buildId] = {
              ...updated[data.buildId],
              currentLog: data.message || data.log || updated[data.buildId].currentLog
            };
          }
        }

        return updated;
      });
    };

    ws.onerror = (error) => {
      console.error("WebSocket error:", error);
    };

    ws.onclose = () => {
      console.log("WebSocket closed");
      setSocket(null);
    };

    return () => {
      ws.close();
    };
  }, [isRunning]);

  // Clean up intervals when component unmounts or test completes
  useEffect(() => {
    return () => {
      if (metricsIntervalRef.current) {
        clearInterval(metricsIntervalRef.current);
      }
      if (pollingIntervalRef.current) {
        clearInterval(pollingIntervalRef.current);
      }
    };
  }, []);

  // Update check for test completion
  useEffect(() => {
    if (
      isRunning &&
      Object.keys(buildStatuses).length === repositories.length
    ) {
      const allCompleted = Object.values(buildStatuses).every(
        (s) =>
          s.status === "success" ||
          s.status === "failed" ||
          s.status === "completed",
      );

      if (allCompleted) {
        setTestComplete(true);
        setIsRunning(false);
        if (metricsIntervalRef.current) {
          clearInterval(metricsIntervalRef.current);
        }
        if (pollingIntervalRef.current) {
          clearInterval(pollingIntervalRef.current);
        }
        setMetrics((prev) => ({
          ...prev,
          testEndTime: Date.now(),
          totalTestDuration: prev.testStartTime
            ? Date.now() - prev.testStartTime
            : 0,
        }));
      }
    }
  }, [buildStatuses, isRunning, repositories.length]);

  // Get status color
  const getStatusColor = (status: string) => {
    switch (status) {
      case "success":
      case "completed":
        return "bg-green-500";
      case "in-progress":
        return "bg-blue-500";
      case "queued":
        return "bg-yellow-500";
      case "failed":
        return "bg-red-500";
      default:
        return "bg-gray-500";
    }
  };

  // Get status icon
  const getStatusIcon = (status: string) => {
    switch (status) {
      case "success":
      case "completed":
        return <CheckCircle className="w-4 h-4" />;
      case "failed":
        return <XCircle className="w-4 h-4" />;
      case "in-progress":
        return <Activity className="w-4 h-4 animate-pulse" />;
      case "queued":
        return <Clock className="w-4 h-4" />;
      default:
        return null;
    }
  };

  // Format duration
  const formatDuration = (ms: number) => {
    const seconds = Math.floor(ms / 1000);
    const minutes = Math.floor(seconds / 60);
    const remainingSeconds = seconds % 60;
    return `${minutes}m ${remainingSeconds}s`;
  };

  // Get build status for repository
  const getBuildForRepo = (repoId: string) => {
    return Object.values(buildStatuses).find(
      (build) => build.repositoryId === repoId,
    );
  };

  const progress = (metrics.completedBuilds / metrics.totalBuilds) * 100 || 0;

  return (
    <div className="container mx-auto px-4 py-6">
      {/* Header */}
      <div className="mb-8">
        <Button
          variant="ghost"
          onClick={() => router.push("/")}
          className="mb-4"
          disabled={isRunning}
        >
          <ArrowLeft className="w-4 h-4 mr-2" />
          Back to Dashboard
        </Button>

        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold mb-2">Throughput Testing</h1>
            <p className="text-gray-600">Test concurrent build performance</p>
          </div>
          <Zap className="w-10 h-10 text-yellow-500" />
        </div>
      </div>

      {/* Repository Configuration */}
      <Card className="mb-6">
        <CardHeader>
          <CardTitle>Repository Configuration</CardTitle>
          <CardDescription>
            Add repositories to test concurrent builds
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex gap-2 mb-4">
            <Input
              placeholder="https://github.com/username/repo"
              value={newRepoUrl}
              onChange={(e) => setNewRepoUrl(e.target.value)}
              onKeyPress={(e) => e.key === "Enter" && addRepository(newRepoUrl)}
              disabled={isRunning}
            />
            <Button
              onClick={() => addRepository(newRepoUrl)}
              disabled={isRunning || !newRepoUrl}
            >
              <Plus className="w-4 h-4 mr-2" />
              Add
            </Button>
            <Dialog open={showBulkDialog} onOpenChange={setShowBulkDialog}>
              <DialogTrigger asChild>
                <Button variant="outline" disabled={isRunning}>
                  <FileText className="w-4 h-4 mr-2" />
                  Bulk Add
                </Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>Add Multiple Repositories</DialogTitle>
                  <DialogDescription>
                    Paste repository URLs, one per line
                  </DialogDescription>
                </DialogHeader>
                <Textarea
                  className="min-h-[200px]"
                  placeholder="https://github.com/user/repo1&#10;https://github.com/user/repo2&#10;..."
                  value={bulkRepos}
                  onChange={(e) => setBulkRepos(e.target.value)}
                />
                <Button onClick={addBulkRepositories}>Add Repositories</Button>
              </DialogContent>
            </Dialog>
          </div>

          {repositories.length > 0 && (
            <>
              <Separator className="my-4" />
              <ScrollArea className="h-[200px]">
                <div className="space-y-2">
                  {repositories.map((repo) => (
                    <div
                      key={repo.id}
                      className="flex items-center justify-between p-2 rounded hover:opacity-80"
                    >
                      <span className="text-sm font-mono">{repo.url}</span>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => removeRepository(repo.id)}
                        disabled={isRunning}
                      >
                        <X className="w-4 h-4" />
                      </Button>
                    </div>
                  ))}
                </div>
              </ScrollArea>
              <Separator className="my-4" />
              <div className="flex justify-between">
                <Button
                  variant="outline"
                  onClick={() => setRepositories([])}
                  disabled={isRunning}
                >
                  <Trash2 className="w-4 h-4 mr-2" />
                  Clear All
                </Button>
                <Button
                  onClick={startTest}
                  disabled={isRunning || repositories.length === 0}
                  className="bg-green-600 hover:bg-green-700"
                >
                  <Play className="w-4 h-4 mr-2" />
                  Start Test ({repositories.length} repos)
                </Button>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Live Metrics */}
      {(isRunning || testComplete) && (
        <>
          {/* Live Metrics */}
          {(isRunning || testComplete) && (
            <>
              <Card className="mb-6">
                <CardHeader>
                  <CardTitle>Live Metrics</CardTitle>
                  <CardDescription>
                    Real-time performance indicators
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="grid grid-cols-4 gap-4 mb-4">
                    <div className="text-center">
                      <div className="text-3xl font-bold">
                        {metrics.runningBuilds}
                      </div>
                      <div className="text-sm text-gray-600">Running</div>
                    </div>
                    <div className="text-center">
                      <div className="text-3xl font-bold text-green-600">
                        {metrics.successfulBuilds}
                      </div>
                      <div className="text-sm text-gray-600">Passed</div>
                    </div>
                    <div className="text-center">
                      <div className="text-3xl font-bold text-red-600">
                        {metrics.failedBuilds}
                      </div>
                      <div className="text-sm text-gray-600">Failed</div>
                    </div>
                    <div className="text-center">
                      <div className="text-3xl font-bold">
                        {metrics.queuedBuilds}
                      </div>
                      <div className="text-sm text-gray-600">Queue</div>
                    </div>
                  </div>
                  <Progress value={progress} className="mb-2" />
                  <div className="text-sm text-gray-600 text-center">
                    {metrics.completedBuilds} of {metrics.totalBuilds} builds
                    completed
                  </div>
                </CardContent>
              </Card>
            </>
          )}

          {/* Build Status Grid */}
          <Card className="mb-6">
            <CardHeader>
              <CardTitle>Build Status</CardTitle>
              <CardDescription>Individual build progress</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
                {repositories.map((repo) => {
                  const build = getBuildForRepo(repo.id);
                  return (
                    <Card key={repo.id} className="relative">
                      <div
                        className={`absolute top-0 left-0 right-0 h-1 ${build ? getStatusColor(build.status) : "bg-gray-300"}`}
                      />
                      <CardHeader className="pb-2 pt-4">
                        <div className="flex items-center justify-between">
                          <CardTitle className="text-sm font-medium truncate">
                            {repo.name}
                          </CardTitle>
                          {build && (
                            <Badge
                              className={`${getStatusColor(build.status)} text-white`}
                            >
                              {getStatusIcon(build.status)}
                            </Badge>
                          )}
                        </div>
                      </CardHeader>
                      <CardContent className="pt-0">
                        <div className="text-xs text-gray-600 truncate font-mono">
                          {build?.currentLog || "Waiting..."}
                        </div>
                        {build?.duration && (
                          <div className="text-xs text-gray-500 mt-1">
                            {formatDuration(build.duration)}
                          </div>
                        )}
                      </CardContent>
                    </Card>
                  );
                })}
              </div>
            </CardContent>
          </Card>

          {/* Final Metrics */}
          {testComplete && (
            <Card>
              <CardHeader>
                <CardTitle>Test Results</CardTitle>
                <CardDescription>Final performance metrics</CardDescription>
              </CardHeader>
              <CardContent>
                <Alert className="mb-4">
                  <AlertTitle>Test Complete</AlertTitle>
                  <AlertDescription>
                    All builds have finished. Total time:{" "}
                    {formatDuration(metrics.totalTestDuration || 0)}
                  </AlertDescription>
                </Alert>

                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Metric</TableHead>
                      <TableHead>Value</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    <TableRow>
                      <TableCell>Total Builds</TableCell>
                      <TableCell>{metrics.totalBuilds}</TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Successful Builds</TableCell>
                      <TableCell className="text-green-600">
                        {metrics.successfulBuilds}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Failed Builds</TableCell>
                      <TableCell className="text-red-600">
                        {metrics.failedBuilds}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Success Rate</TableCell>
                      <TableCell>
                        {(
                          (metrics.successfulBuilds / metrics.totalBuilds) *
                          100
                        ).toFixed(1)}
                        %
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Average Build Time</TableCell>
                      <TableCell>
                        {formatDuration(metrics.averageDuration)}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Min Build Time</TableCell>
                      <TableCell>
                        {formatDuration(metrics.minDuration)}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Max Build Time</TableCell>
                      <TableCell>
                        {formatDuration(metrics.maxDuration)}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Throughput</TableCell>
                      <TableCell className="font-bold">
                        {metrics.throughput.toFixed(2)} builds/min
                      </TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}
        </>
      )}
    </div>
  );
}
