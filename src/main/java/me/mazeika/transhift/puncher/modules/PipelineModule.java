package me.mazeika.transhift.puncher.modules;

import com.google.inject.AbstractModule;
import me.mazeika.transhift.puncher.pipeline.Pipeline;
import me.mazeika.transhift.puncher.pipeline.PipelineImpl;

import javax.inject.Singleton;

public class PipelineModule extends AbstractModule
{
    @Override
    protected void configure()
    {
        bind(Pipeline.class)
                .annotatedWith(Pipeline.Shutdown.class)
                .to(PipelineImpl.class)
                .in(Singleton.class);
    }
}
