# Marching Cubes calculated on GPU

A marching cube algorithm, that is executed in parallel on the GPU, using compute shaders.
This will later enable a highly parallel creation of advanced landscape/terrain structures in potentially real-time (next project).

Some current results are already quite nice. I am currently able to (quite perfomant!) run marching cubes on 1000 cubes with about 800fps
on an Nvidia Quadro 3000M.

It is also possible to add/include any kind of implicit function that is then correctly triangulated and drawn.

The following example screenshots show an example of intersection, union and difference for the implicit functions of sphere, cubes and cylinders.
The low-poly look will disappear with more live-generated cubes later.

![triangulation of cubes/spheres/cylinders](https://github.com/MauriceGit/Marching_Cubes_on_GPU/blob/master/Screenshots/cube_sphere_cylinder_example1.png "cubes/spheres and cylinders")
![triangulation of cubes/spheres/cylinders](https://github.com/MauriceGit/Marching_Cubes_on_GPU/blob/master/Screenshots/cube_sphere_cylinder_example2.png "cubes/spheres and cylinders")
![triangulation of cubes/spheres/cylinders](https://github.com/MauriceGit/Marching_Cubes_on_GPU/blob/master/Screenshots/wireframe_example1.png "cubes/spheres and cylinders")
![triangulation of cubes/spheres/cylinders](https://github.com/MauriceGit/Marching_Cubes_on_GPU/blob/master/Screenshots/wireframe_example2.png "cubes/spheres and cylinders")
