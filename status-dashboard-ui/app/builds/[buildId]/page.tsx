"use client";

import { useEffect, useState, use } from "react";
import { useRouter } from "next/navigation";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { ArrowLeft, Download, RefreshCw } from "lucide-react";

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

interface PageProps {
    params: Promise<{ buildId: string }>;
}

export default function BuildDetailsPage({ params }: PageProps) {
    const resolvedParams = use(params);
    const buildId = resolvedParams.buildId;
    const router = useRouter();
    const [build, setBuild] = useState<BuildStatus | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [socket, setSocket] = useState<WebSocket | null>(null);

    // Fetch initial build data
    useEffect(() => {
        const fetchBuildDetails = async () => {
            try {
                const token = localStorage.getItem("authToken");
                const response = await fetch(`http://localhost:8086/api/builds/${buildId}`, {
                    headers: token ? {
                        "Authorization": `Bearer ${token}`
                    } : {}
                });

                if (!response.ok) {
                    throw new Error("Failed to fetch build details");
                }

                const data = await response.json();
                console.log("Fetched build details:", data);
                setBuild(data);
            } catch (err) {
                setError(err instanceof Error ? err.message : "An error occurred");
            } finally {
                setLoading(false);
            }
        };

        fetchBuildDetails();
    }, [buildId]);

    // Set up WebSocket connection for live updates
    useEffect(() => {
        const clientId = `build-details-${buildId}-${Date.now()}`;
        const ws = new WebSocket(`ws://localhost:8085/ws?clientId=${clientId}&buildId=${buildId}`);

        ws.onopen = () => {
            console.log("WebSocket connection established for build:", buildId);
            setSocket(ws);
        };

        ws.onmessage = (event) => {
            const data = JSON.parse(event.data);

            if (data.buildId !== buildId) return;

            if (data.type === "status") {
                setBuild(prevBuild => {
                    if (!prevBuild) return null;
                    return {
                        ...prevBuild,
                        status: data.status,
                        message: data.message,
                        updated_at: data.time
                    };
                });
            } else if (data.type === "log") {
                setBuild(prevBuild => {
                    if (!prevBuild) return null;
                    return {
                        ...prevBuild,
                        logs: [...(prevBuild.logs || []), data.log]
                    };
                });
            } else if (data.type === "completion") {
                setBuild(prevBuild => {
                    if (!prevBuild) return null;
                    return {
                        ...prevBuild,
                        status: data.status,
                        artifact_url: data.artifactUrl,
                        updated_at: data.time
                    };
                });
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
    }, [buildId]);

    // Auto-scroll logs to bottom when new logs arrive
    useEffect(() => {
        const logContainer = document.querySelector('.overflow-y-auto');
        if (logContainer && build?.logs?.length) {
            logContainer.scrollTop = logContainer.scrollHeight;
        }
    }, [build?.logs]);

    const getStatusColor = (status: string) => {
        switch (status) {
            case "completed":
            case "success":
                return "bg-green-500";
            case "in-progress":
                return "bg-blue-500";
            case "queued":
                return "bg-yellow-500";
            case "failed":
            case "failure":
                return "bg-red-500";
            default:
                return "bg-gray-500";
        }
    };

    const getStatusIcon = (status: string) => {
        switch (status) {
            case "in-progress":
                return <RefreshCw className="w-4 h-4 animate-spin" />;
            default:
                return null;
        }
    };

    if (loading) {
        return (
            <div className="container mx-auto py-10">
                <div className="flex items-center justify-center min-h-[60vh]">
                    <div className="text-center">
                        <RefreshCw className="w-8 h-8 animate-spin mx-auto mb-4" />
                        <p>Loading build details...</p>
                    </div>
                </div>
            </div>
        );
    }

    if (error || !build) {
        return (
            <div className="container mx-auto py-10">
                <Alert variant="destructive">
                    <AlertTitle>Error</AlertTitle>
                    <AlertDescription>{error || "Build not found"}</AlertDescription>
                </Alert>
                <Button
                    className="mt-4"
                    variant="outline"
                    onClick={() => router.push("/")}
                >
                    <ArrowLeft className="w-4 h-4 mr-2" />
                    Back to Dashboard
                </Button>
            </div>
        );
    }

    return (
        <div className="container mx-auto py-10">
            {/* Header */}
            <div className="mb-8">
                <Button
                    variant="ghost"
                    onClick={() => router.push("/")}
                    className="mb-4"
                >
                    <ArrowLeft className="w-4 h-4 mr-2" />
                    Back to Dashboard
                </Button>

                <div className="flex items-center justify-between">
                    <div>
                        <h1 className="text-3xl font-bold mb-2">Build Details</h1>
                        <p className="text-gray-600">ID: {build.id}</p>
                    </div>
                    <Badge className={`${getStatusColor(build.status)} text-white px-4 py-2`}>
            <span className="flex items-center gap-2">
              {getStatusIcon(build.status)}
                {build.status}
            </span>
                    </Badge>
                </div>
            </div>

            {/* Build Information */}
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 mb-8">
                <Card className="lg:col-span-2">
                    <CardHeader>
                        <CardTitle>Repository Information</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-2">
                        <div>
                            <span className="font-semibold">Repository:</span>{" "}
                            <a
                                href={build.repository_url}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-blue-500 hover:underline"
                            >
                                {build.repository_url}
                            </a>
                        </div>
                        {build.branch && (
                            <div>
                                <span className="font-semibold">Branch:</span> {build.branch}
                            </div>
                        )}
                        {build.commit_hash && (
                            <div>
                                <span className="font-semibold">Commit:</span>{" "}
                                <code className="bg-gray-100 px-2 py-1 rounded text-sm">
                                    {build.commit_hash}
                                </code>
                            </div>
                        )}
                        {build.message && (
                            <div>
                                <span className="font-semibold">Message:</span> {build.message}
                            </div>
                        )}
                    </CardContent>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle>Build Timeline</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-2 text-sm">
                        <div>
                            <span className="font-semibold">Created:</span>{" "}
                            {new Date(build.created_at).toLocaleString()}
                        </div>
                        <div>
                            <span className="font-semibold">Last Updated:</span>{" "}
                            {new Date(build.updated_at).toLocaleString()}
                        </div>
                        {build.status === "completed" || build.status === "success" || build.status === "failed" ? (
                            <div>
                                <span className="font-semibold">Duration:</span>{" "}
                                {(() => {
                                    const duration = new Date(build.updated_at).getTime() - new Date(build.created_at).getTime();
                                    const minutes = Math.floor(duration / 60000);
                                    const seconds = Math.floor((duration % 60000) / 1000);
                                    return `${minutes}m ${seconds}s`;
                                })()}
                            </div>
                        ) : null}
                    </CardContent>
                </Card>
            </div>

            {/* Build Logs */}
            <Card>
                <CardHeader>
                    <div className="flex items-center justify-between">
                        <CardTitle>Build Logs</CardTitle>
                        {socket && build.status === "in-progress" && (
                            <Badge variant="outline" className="animate-pulse">
                <span className="flex items-center gap-2">
                  <span className="w-2 h-2 bg-green-500 rounded-full"></span>
                  Live
                </span>
                            </Badge>
                        )}
                    </div>
                    <CardDescription>
                        Real-time build output and logs
                    </CardDescription>
                </CardHeader>
                <CardContent>
                    <div className="bg-black text-green-400 p-4 rounded-lg font-mono text-sm h-96 overflow-y-auto">
                        {build.logs && build.logs.length > 0 ? (
                            build.logs.map((log, index) => (
                                <div key={index} className="mb-1">
                                    <span className="text-gray-500">[{index.toString().padStart(4, '0')}]</span> {log}
                                </div>
                            ))
                        ) : (
                            <p className="text-gray-500">Waiting for logs...</p>
                        )}
                    </div>
                </CardContent>
            </Card>

            {/* Download Artifact Button */}
            {build.artifact_url && (
                <div className="mt-6 flex justify-center">
                    <Button asChild size="lg">
                        {/*doing a replace here is a not so good workaround and could/will cause problems on other systems, but I don't want to fetch it from the backend first then load it here and download it for the user..*/}
<a
                        href={build.artifact_url.replace("storage", "localhost")}
                        target="_blank"
                        rel="noopener noreferrer"
                        >
                        <Download className="w-4 h-4 mr-2" />
                        Download Build Artifact
                    </a>
                </Button>
                </div>
                )}
</div>
);
}