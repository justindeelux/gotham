import{f as u}from"./format-length-BCzgjvNx.js";import{Z as v,an as l,Y as d,d as y,_ as h,cS as C,$ as m,a0 as _,a2 as b,ap as $,g as a,d9 as z}from"./index-D_xLTFMx.js";var S=v("icon",`
 height: 1em;
 width: 1em;
 line-height: 1em;
 text-align: center;
 display: inline-block;
 position: relative;
 fill: currentColor;
`,[l("color-transition",{transition:"color .3s var(--n-bezier)"}),l("depth",{color:"var(--n-color)"},[d("svg",{opacity:"var(--n-opacity)",transition:"opacity .3s var(--n-bezier)"})]),d("svg",{height:"1em",width:"1em"})]);const x={...h.props,depth:[String,Number],size:[Number,String],color:String,component:[Object,Function]},w=y({_n_icon__:!0,name:"Icon",inheritAttrs:!1,props:x,setup(e){const{mergedClsPrefixRef:o,inlineThemeDisabled:n}=b(e),t=h("Icon","-icon",S,z,e,o),i=a(()=>{const{depth:r}=e,{common:{cubicBezierEaseInOut:c},self:p}=t.value;if(r!==void 0){const{color:f,[`opacity${r}Depth`]:g}=p;return{"--n-bezier":c,"--n-color":f,"--n-opacity":g}}return{"--n-bezier":c,"--n-color":"","--n-opacity":""}}),s=n?$("icon",a(()=>`${e.depth||"d"}`),i,e):void 0;return{mergedClsPrefix:o,mergedStyle:a(()=>{const{size:r,color:c}=e;return{fontSize:u(r),color:c}}),cssVars:n?void 0:i,themeClass:s?.themeClass,onRender:s?.onRender}},render(){const{$parent:e,depth:o,mergedClsPrefix:n,component:t,onRender:i,themeClass:s}=this;return e?.$options?._n_icon__&&C("icon","don't wrap `n-icon` inside `n-icon`"),i?.(),m("i",_(this.$attrs,{role:"img",class:[`${n}-icon`,s,{[`${n}-icon--depth`]:o,[`${n}-icon--color-transition`]:o!==void 0}],style:[this.cssVars,this.mergedStyle]}),t?m(t):this.$slots.default?.())}});export{w as N};
