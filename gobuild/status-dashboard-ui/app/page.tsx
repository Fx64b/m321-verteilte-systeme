"use client";

import { useEffect, useState } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

type BuildStatus = {
  id: string;
  status: string;
  logs: string;
};

export default function Dashboard() {
  const [builds, setBuilds] = useState<BuildStatus[]>([]);
  const [loading, setLoading] = useState(true);

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
    // In a real app, you might want to set up a WebSocket connection here
  }, []);

  const getStatusColor = (status: string) => {
    switch (status) {
      case "completed":
        return "bg-green-500";
      case "in-progress":
        return "bg-blue-500";
      case "failed":
        return "bg-red-500";
      default:
        return "bg-gray-500";
    }
  };

  return (
      <div className="container mx-auto py-10">
        <h1 className="text-3xl font-bold mb-8">GoBuild Status Dashboard</h1>

        {loading ? (
            <p>Loading builds...</p>
        ) : (
            <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
              {builds.map((build) => (
                  <Card key={build.id}>
                    <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                      <CardTitle className="text-sm font-medium">Build {build.id}</CardTitle>
                      <Badge className={getStatusColor(build.status)}>
                        {build.status}
                      </Badge>
                    </CardHeader>
                    <CardContent>
                      <CardDescription className="font-mono whitespace-pre-wrap text-xs">
                        {build.logs}
                      </CardDescription>
                    </CardContent>
                  </Card>
              ))}
            </div>
        )}
      </div>
  );
}